package testrigor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// --- buildModuleState: quarantine, .only shadowing, score rounding (pure) ----

func TestBuildModuleStateQuarantinesFlaky(t *testing.T) {
	// A flaky contract test that "killed" all its mutants must NOT count: the
	// score comes from the non-flaky test only, and the contract is flagged flaky.
	mr := moduleReport{
		Module:    "src/checkout",
		Coverage:  90,
		MockRatio: 20,
		Tests: []testReport{
			{ID: "c::boundary", Behaviors: []string{"checkout"}, Contract: true, Flaky: true, MutantsInScope: 10, MutantsKilled: 10, File: "src/checkout/c.spec.ts", Line: 3},
			{ID: "u::unit", Behaviors: []string{"checkout"}, MutantsInScope: 10, MutantsKilled: 6, File: "src/checkout/u.spec.ts", Line: 1},
		},
	}
	st := buildModuleState(plane.ModuleRef{ID: "src/checkout", Language: "typescript"}, mr, true)

	if st.MutationScore != 60 { // 6/10 non-flaky only; the flaky 10/10 is excluded
		t.Errorf("score = %d, want 60 (flaky excluded)", st.MutationScore)
	}
	if !st.ContractFlaky {
		t.Error("contract should be flagged flaky")
	}
	if st.ContractKilled != 0 || st.ContractMutants != 0 {
		t.Errorf("flaky contract must contribute no kills: killed=%d mutants=%d", st.ContractKilled, st.ContractMutants)
	}
}

func TestBuildModuleStateOnlyShadows(t *testing.T) {
	// A `.only` test silently disables its siblings: they must read as skipped.
	mr := moduleReport{Module: "m", Tests: []testReport{
		{ID: "a", Only: true},
		{ID: "b"},
	}}
	st := buildModuleState(plane.ModuleRef{ID: "m", Language: "typescript"}, mr, true)
	byID := map[string]TestState{}
	for _, ts := range st.Tests {
		byID[ts.ID] = ts
	}
	if byID["b"].Skipped != true {
		t.Error("sibling of .only must be effectively skipped")
	}
	if byID["a"].Skipped != false {
		t.Error(".only test itself is not skipped")
	}
}

func TestPctRoundsHalfUp(t *testing.T) {
	cases := []struct{ k, n, want int }{{0, 10, 0}, {10, 10, 100}, {12, 15, 80}, {1, 3, 33}, {2, 3, 67}, {0, 0, 0}}
	for _, c := range cases {
		if got := pct(c.k, c.n); got != c.want {
			t.Errorf("pct(%d,%d) = %d, want %d", c.k, c.n, got, c.want)
		}
	}
}

// --- Derive end to end (with temp repo + counting runner) --------------------

const tsReport = `{"tool":{"name":"stryker","version":"8.2.0"},"modules":[
  {"module":"src/checkout","coverage":92,"mockRatio":20,"tests":[
    {"id":"checkout.spec::pays","behaviors":["checkout"],"contract":true,"mutantsInScope":10,"mutantsKilled":8,"file":"src/checkout/checkout.spec.ts","line":3}
  ]}]}`

func TestDeriveCacheSkipsUnchangedThenRerunsOnChange(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"src/checkout/index.ts":         "export const pay = () => 1\n",
		"src/checkout/checkout.spec.ts": "test('pays', () => {})\n",
	})
	runner := newStubRunner()
	runner.reports[toolTypeScript] = []byte(tsReport)
	shared := NewMemoryCache()
	p := New(func(string) Cache { return shared })
	svc := deriveSvc(root, runner, map[string][]string{"src/checkout": {"src/checkout/index.ts", "src/checkout/checkout.spec.ts"}})
	mods := []plane.ModuleRef{{ID: "src/checkout", Language: "typescript"}}

	// Cold: the mutation tool runs once.
	m1, err := p.derive(context.Background(), mods, svc)
	if err != nil {
		t.Fatal(err)
	}
	if runner.runs[toolTypeScript] != 1 {
		t.Fatalf("cold run: tool calls = %d, want 1", runner.runs[toolTypeScript])
	}

	// Warm: identical content → cache hit → the tool is NOT re-run.
	m2, err := p.derive(context.Background(), mods, svc)
	if err != nil {
		t.Fatal(err)
	}
	if runner.runs[toolTypeScript] != 1 {
		t.Fatalf("warm run: tool calls = %d, want still 1 (cache hit)", runner.runs[toolTypeScript])
	}
	if digest(t, m1) != digest(t, m2) {
		t.Fatal("cached derive is not byte-identical to fresh derive (determinism broken)")
	}

	// Change a source file → hash changes → the tool re-runs.
	overwrite(t, root, "src/checkout/index.ts", "export const pay = () => 2\n")
	if _, err := p.derive(context.Background(), mods, svc); err != nil {
		t.Fatal(err)
	}
	if runner.runs[toolTypeScript] != 2 {
		t.Fatalf("after change: tool calls = %d, want 2", runner.runs[toolTypeScript])
	}
}

func TestDeriveToolMissingIsFailClosed(t *testing.T) {
	root := writeRepo(t, map[string]string{"src/checkout/index.ts": "x\n"})
	runner := newStubRunner()
	runner.missing[toolTypeScript] = "install Node + Stryker"
	p := New(func(string) Cache { return NewMemoryCache() })
	svc := deriveSvc(root, runner, map[string][]string{"src/checkout": {"src/checkout/index.ts"}})

	_, err := p.derive(context.Background(), []plane.ModuleRef{{ID: "src/checkout", Language: "typescript"}}, svc)
	if err == nil {
		t.Fatal("missing mutation tool must fail closed, not pass")
	}
	var tm *plane.ToolMissingError
	if !errors.As(err, &tm) {
		t.Fatalf("want ToolMissingError, got %v", err)
	}
}

func TestDeriveUnsupportedLanguageIsFailClosed(t *testing.T) {
	root := writeRepo(t, map[string]string{"lib/x.go": "x\n"})
	p := New(func(string) Cache { return NewMemoryCache() })
	svc := deriveSvc(root, newStubRunner(), map[string][]string{"lib": {"lib/x.go"}})
	if _, err := p.derive(context.Background(), []plane.ModuleRef{{ID: "lib", Language: "go"}}, svc); err == nil {
		t.Fatal("a language with no mutation helper must fail closed")
	}
}

func TestBaselineReplayAndAbsent(t *testing.T) {
	root := writeRepo(t, map[string]string{"src/checkout/index.ts": "x\n"})
	runner := newStubRunner()
	runner.reports[toolTypeScript] = []byte(tsReport)
	p := New(func(string) Cache { return NewMemoryCache() })
	svc := deriveSvc(root, runner, map[string][]string{"src/checkout": {"src/checkout/index.ts"}})
	mods := []plane.ModuleRef{{ID: "src/checkout", Language: "typescript"}}

	// Absent baseline → nil (benign, comparison rules disabled).
	m, err := p.derive(context.Background(), mods, svc)
	if err != nil {
		t.Fatal(err)
	}
	if m.Baseline != nil {
		t.Fatalf("absent baseline must be nil, got %+v", m.Baseline)
	}

	// Present baseline → replayed into the model.
	runner.reports[baselineTool] = []byte(`{"modules":[{"module":"src/checkout","threshold":80,"mutationScore":90,"mockRatio":10,"requiredTests":{"checkout":["checkout.spec::pays"]}}]}`)
	m2, err := p.derive(context.Background(), mods, svc)
	if err != nil {
		t.Fatal(err)
	}
	bl := m2.Baseline["src/checkout"]
	if bl == nil || !bl.HasThreshold || bl.Threshold != 80 || bl.RequiredTests["checkout"][0] != "checkout.spec::pays" {
		t.Fatalf("baseline not replayed: %+v", bl)
	}
}

func digest(t *testing.T, m *Model) string {
	t.Helper()
	b, err := json.Marshal(m.Modules)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
