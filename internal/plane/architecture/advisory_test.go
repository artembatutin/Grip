package architecture

import (
	"context"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// TestAdvisoryRules pins each Tier B rule with a positive fixture and a negative
// near-miss (plan/08: every deterministic rule needs both). Every advisory is
// Tier B, so it is reported but non-blocking — the acceptance matrix proves the
// non-blocking/promote behavior end to end; here we prove the rule logic and the
// threshold boundaries.
func TestAdvisoryRules(t *testing.T) {
	gov := intents(
		in("src/a", nil, nil, ""),
		in("src/b", nil, nil, ""),
	)
	govWithDep := intents(
		in("src/a", nil, []string{"src/b"}, ""), // a declares a dependency on b
		in("src/b", nil, nil, ""),
	)
	dupLocs := []Loc{{Module: "src/a", File: "src/a/x.ts", Line: 1}, {Module: "src/b", File: "src/b/y.ts", Line: 9}}

	cases := []struct {
		name       string
		intents    map[string]Intent
		signals    Signals
		wantRule   string // rule id expected exactly once ("" = none)
		wantMsgSub string
	}{
		// duplication
		{
			name:       "duplication fires across modules",
			intents:    gov,
			signals:    Signals{Duplications: []DuplicationSignal{{Lines: 12, Modules: []string{"src/a", "src/b"}, Locs: dupLocs}}},
			wantRule:   RuleDuplication,
			wantMsgSub: "share 12 lines of duplicated code",
		},
		{
			name:    "duplication near-miss: single module",
			intents: gov,
			signals: Signals{Duplications: []DuplicationSignal{{Lines: 12, Modules: []string{"src/a"}, Locs: dupLocs[:1]}}},
		},
		{
			name:    "duplication near-miss: too few lines",
			intents: gov,
			signals: Signals{Duplications: []DuplicationSignal{{Lines: 4, Modules: []string{"src/a", "src/b"}, Locs: dupLocs}}},
		},
		// co-change
		{
			name:       "co-change fires on hidden coupling",
			intents:    gov,
			signals:    Signals{CoChanges: []CoChangeSignal{{A: "src/a", B: "src/b", Together: 8, Total: 10}}},
			wantRule:   RuleCoChange,
			wantMsgSub: "changed together in 8 of 10 commits",
		},
		{
			name:    "co-change near-miss: declared dependency",
			intents: govWithDep,
			signals: Signals{CoChanges: []CoChangeSignal{{A: "src/a", B: "src/b", Together: 8, Total: 10}}},
		},
		{
			name:    "co-change near-miss: below commit floor",
			intents: gov,
			signals: Signals{CoChanges: []CoChangeSignal{{A: "src/a", B: "src/b", Together: 2, Total: 2}}},
		},
		{
			name:    "co-change near-miss: below ratio",
			intents: gov,
			signals: Signals{CoChanges: []CoChangeSignal{{A: "src/a", B: "src/b", Together: 3, Total: 10}}},
		},
		{
			name:    "co-change near-miss: a module is ungoverned",
			intents: intents(in("src/a", nil, nil, "")),
			signals: Signals{CoChanges: []CoChangeSignal{{A: "src/a", B: "src/b", Together: 8, Total: 10}}},
		},
		// middle-man
		{
			name:       "middle-man fires at the delegation ratio",
			intents:    gov,
			signals:    Signals{MiddleMen: []MiddleManSignal{{Module: "src/a", Forwards: 6, Methods: 8}}},
			wantRule:   RuleMiddleMan,
			wantMsgSub: "forwards 6 of its 8 methods",
		},
		{
			name:    "middle-man near-miss: below ratio",
			intents: gov,
			signals: Signals{MiddleMen: []MiddleManSignal{{Module: "src/a", Forwards: 5, Methods: 8}}},
		},
		{
			name:    "middle-man near-miss: too few methods",
			intents: gov,
			signals: Signals{MiddleMen: []MiddleManSignal{{Module: "src/a", Forwards: 3, Methods: 3}}},
		},
		// message chains
		{
			name:       "message-chain fires at threshold",
			intents:    gov,
			signals:    Signals{Chains: []ChainSignal{{Module: "src/a", File: "src/a/x.ts", Line: 5, Length: 4}}},
			wantRule:   RuleMessageChains,
			wantMsgSub: "message chain of length 4",
		},
		{
			name:    "message-chain near-miss: below threshold",
			intents: gov,
			signals: Signals{Chains: []ChainSignal{{Module: "src/a", File: "src/a/x.ts", Line: 5, Length: 3}}},
		},
		// speculative generality
		{
			name:       "speculative-generality fires on single implementor",
			intents:    gov,
			signals:    Signals{Abstractions: []AbstractionSignal{{Name: "Repo", Module: "src/a", File: "src/a/x.ts", Line: 2, Implementors: 1}}},
			wantRule:   RuleSpeculativeGenerality,
			wantMsgSub: "has a single implementor",
		},
		{
			name:    "speculative-generality near-miss: two implementors",
			intents: gov,
			signals: Signals{Abstractions: []AbstractionSignal{{Name: "Repo", Module: "src/a", File: "src/a/x.ts", Line: 2, Implementors: 2}}},
		},
		// complexity
		{
			name:       "complexity fires above threshold",
			intents:    gov,
			signals:    Signals{Complexity: []ComplexitySignal{{Function: "handle", Module: "src/a", File: "src/a/x.ts", Line: 20, Complexity: 15}}},
			wantRule:   RuleComplexity,
			wantMsgSub: "cyclomatic complexity 15",
		},
		{
			name:    "complexity near-miss: at threshold",
			intents: gov,
			signals: Signals{Complexity: []ComplexitySignal{{Function: "handle", Module: "src/a", File: "src/a/x.ts", Line: 20, Complexity: 10}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := advisoryViolations(tc.intents, tc.signals)
			if tc.wantRule == "" {
				if len(got) != 0 {
					t.Fatalf("expected no advisory, got %v", ruleIDs(got))
				}
				return
			}
			if len(got) != 1 || got[0].RuleID != tc.wantRule {
				t.Fatalf("expected exactly rule %q, got %v", tc.wantRule, ruleIDs(got))
			}
			v := got[0]
			if v.Tier != plane.TierB {
				t.Errorf("advisory %q must be Tier B, got %s", v.RuleID, v.Tier)
			}
			if v.Plane != PlaneID {
				t.Errorf("advisory %q missing plane id", v.RuleID)
			}
			if tc.wantMsgSub != "" && !strings.Contains(v.Message, tc.wantMsgSub) {
				t.Errorf("message %q missing %q", v.Message, tc.wantMsgSub)
			}
		})
	}
}

// fakeRunner is a plane.ToolRunner that returns a canned advisory report (or an
// error) for the advisory helper, so the best-effort derivation is exercised
// offline.
type fakeRunner struct {
	report string
	err    error
}

func (f fakeRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if name == advisoryHelper {
		return []byte(f.report), nil
	}
	return []byte("{}"), nil
}
func (f fakeRunner) Version(ctx context.Context, name string) (string, error) { return "test", nil }

func testModuleOf(file string) string {
	switch {
	case strings.HasPrefix(file, "src/a/"):
		return "src/a"
	case strings.HasPrefix(file, "src/b/"):
		return "src/b"
	default:
		return "" // ungoverned
	}
}

// TestDeriveAdvisoryMapsFilesToModules proves the derivation resolves file-located
// facts to governed modules and drops occurrences outside any module.
func TestDeriveAdvisoryMapsFilesToModules(t *testing.T) {
	const report = `{
	  "clones": [
	    {"lines": 20, "occurrences": [
	      {"file": "src/a/x.ts", "line": 3},
	      {"file": "src/b/y.ts", "line": 8},
	      {"file": "vendor/z.ts", "line": 1}
	    ]}
	  ],
	  "complexity": [
	    {"function": "handle", "file": "src/a/x.ts", "line": 40, "complexity": 14},
	    {"function": "ignored", "file": "vendor/z.ts", "line": 1, "complexity": 99}
	  ]
	}`
	svc := plane.DeriveServices{Tools: fakeRunner{report: report}, ModuleOf: testModuleOf}
	sig := deriveAdvisory(context.Background(), svc)

	if len(sig.Duplications) != 1 {
		t.Fatalf("want 1 duplication, got %d", len(sig.Duplications))
	}
	d := sig.Duplications[0]
	if len(d.Locs) != 2 { // vendor occurrence dropped
		t.Errorf("want 2 governed occurrences, got %d", len(d.Locs))
	}
	if strings.Join(d.Modules, ",") != "src/a,src/b" {
		t.Errorf("want modules src/a,src/b, got %v", d.Modules)
	}
	if len(sig.Complexity) != 1 || sig.Complexity[0].Module != "src/a" { // vendor complexity dropped
		t.Errorf("complexity not resolved to governed module: %+v", sig.Complexity)
	}
}

// TestDeriveAdvisoryIsBestEffort proves advisories never fail the gate: a nil
// ToolRunner and a failing ToolRunner both yield empty signals, no panic, no error.
func TestDeriveAdvisoryIsBestEffort(t *testing.T) {
	if s := deriveAdvisory(context.Background(), plane.DeriveServices{ModuleOf: testModuleOf}); !s.Empty() {
		t.Error("nil ToolRunner should yield no advisories")
	}
	failing := plane.DeriveServices{Tools: fakeRunner{err: context.Canceled}, ModuleOf: testModuleOf}
	if s := deriveAdvisory(context.Background(), failing); !s.Empty() {
		t.Error("failing ToolRunner should yield no advisories, not an error")
	}
}

// TestNormalizeAdvisoryDeterministic proves the advisory normalization is
// order-independent: two reports with the same records in reversed order produce
// byte-identical signals and identical Tier B violations (NFR-1).
func TestNormalizeAdvisoryDeterministic(t *testing.T) {
	rep := AdvisoryReport{
		Complexity: []ComplexityRec{
			{Function: "a", File: "src/a/1.ts", Line: 1, Complexity: 20},
			{Function: "b", File: "src/b/2.ts", Line: 2, Complexity: 30},
		},
		CoChanges: []CoChangeRec{
			{ModuleA: "src/b", ModuleB: "src/a", Together: 9, Total: 10},
			{ModuleA: "src/a", ModuleB: "src/b", Together: 9, Total: 10},
		},
	}
	rev := AdvisoryReport{
		Complexity: []ComplexityRec{rep.Complexity[1], rep.Complexity[0]},
		CoChanges:  []CoChangeRec{rep.CoChanges[1], rep.CoChanges[0]},
	}
	is := intents(in("src/a", nil, nil, ""), in("src/b", nil, nil, ""))
	forward := ruleLines(advisoryViolations(is, normalizeAdvisory(&rep, testModuleOf)))
	reverse := ruleLines(advisoryViolations(is, normalizeAdvisory(&rev, testModuleOf)))
	if strings.Join(forward, "\n") != strings.Join(reverse, "\n") {
		t.Fatalf("advisory output is order-dependent:\n%v\nvs\n%v", forward, reverse)
	}
}

func ruleLines(vs []plane.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.RuleID+"|"+v.Message)
	}
	return out
}
