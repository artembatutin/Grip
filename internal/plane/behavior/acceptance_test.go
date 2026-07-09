package behavior_test

// This is the end-to-end M2 gate: scripted "agent" diffs over synthetic PHP+TS
// fixtures run through the REAL gate (cli.BuildRegistry → config → discovery →
// the generic plane loop → behavior Derive/Reconcile → decision → report),
// entirely offline via recorded capture reports and git-tracked snapshot files. It
// proves the plane works through the UNTOUCHED engine — behavior is registered in
// cli and nothing in internal/gate|reconcile|config|ir changed — and pins the
// exact report strings via golden files. It lives in an external test package so
// it can import cli without an import cycle.

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/manifest"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/plane/behavior"
	"github.com/artembatutin/grip/internal/report"
)

var update = flag.Bool("update", false, "update golden report files")

// --- fixture repo (base) ----------------------------------------------------

const gripConfig = `version: 1
planes:
  behavior: { enabled: true }
languages:
  typescript:
    roots: ["src"]
    tool: { name: dependency-cruiser }
  php:
    roots: ["app"]
    tool: { name: deptrac }
modules:
  granularity: directory
gate:
  failClosed: true
  local: { planes: [behavior] }
  ci: { planes: [behavior] }
`

// base: two behavior-governed modules (TS checkout pins placeOrder; PHP refund
// pins refund). Capture reports carry timestamps/addresses so normalization is
// exercised on the happy path. Pins are written by the harness from this capture,
// so the base is a clean pass by construction.
func baseFiles() map[string]string {
	return map[string]string{
		".grip.yaml": gripConfig,

		"src/checkout/grip.yaml": "module: checkout\nbehavior:\n  pin: [placeOrder]\n",
		"src/checkout/index.ts":  "export const placeOrder = () => 'ok'\n",

		"app/Refund/grip.yaml":  "module: refund\nbehavior:\n  pin: [refund]\n",
		"app/Refund/Refund.php": "<?php class Refund {}\n",

		".grip-analysis/behavior-typescript.json": tsBase,
		".grip-analysis/behavior-php.json":        phpBase,
	}
}

const tsBase = `{"tool":{"name":"grip-behavior-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","boundaries":[
    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"cases":[
      {"name":"empty-cart","output":"error: cart is empty"},
      {"name":"happy","output":"order 0x41b2 placed at 2024-01-02T03:04:05Z"}]}]}]}`

// tsPreserving: only the timestamp and address changed (behavior-preserving).
const tsPreserving = `{"tool":{"name":"grip-behavior-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","boundaries":[
    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"cases":[
      {"name":"empty-cart","output":"error: cart is empty"},
      {"name":"happy","output":"order 0x99ff placed at 2025-09-09T09:09:09Z"}]}]}]}`

// tsChanged: the empty-cart message changed (a real observable-output change).
const tsChanged = `{"tool":{"name":"grip-behavior-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","boundaries":[
    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"cases":[
      {"name":"empty-cart","output":"error: your cart has no items"},
      {"name":"happy","output":"order 0x41b2 placed at 2024-01-02T03:04:05Z"}]}]}]}`

// tsFlaky: the capture helper flagged the boundary nondeterministic.
const tsFlaky = `{"tool":{"name":"grip-behavior-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","boundaries":[
    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"nondeterministic":true,"cases":[]}]}]}`

// tsWithNew: observes placeOrder plus a second boundary `total`.
const tsWithNew = `{"tool":{"name":"grip-behavior-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","boundaries":[
    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"cases":[
      {"name":"empty-cart","output":"error: cart is empty"},
      {"name":"happy","output":"order 0x41b2 placed at 2024-01-02T03:04:05Z"}]},
    {"name":"total","file":"src/checkout/index.ts","line":8,"cases":[
      {"name":"sums","output":"total = 1299"}]}]}]}`

const phpBase = `{"tool":{"name":"grip-behavior-php","version":"1.0.0"},"modules":[
  {"module":"app/Refund","boundaries":[
    {"name":"refund","file":"app/Refund/Refund.php","line":5,"cases":[
      {"name":"happy","output":"refunded 500 to card 0x7ac1"}]}]}]}`

// --- harness ---------------------------------------------------------------

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if content == "" {
			_ = os.Remove(abs)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newRepo(t *testing.T, overrides map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, baseFiles())
	if overrides != nil {
		writeFiles(t, root, overrides)
	}
	return root
}

func analysisDir(root string) string { return filepath.Join(root, ".grip-analysis") }

// pin captures the current recorded behavior and writes the snapshot files for the
// named modules — exactly what `grip ratify behavior` does. This is how a scenario
// establishes the "already ratified" baseline before simulating an agent's change.
func pin(t *testing.T, root string, modules ...string) {
	t.Helper()
	derived := deriveBehavior(t, root)
	for _, mod := range modules {
		for _, f := range behavior.SnapshotsFor(derived, mod, nil) {
			if f.Reduced {
				continue
			}
			abs := filepath.Join(root, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte(f.Content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// deriveBehavior runs just the behavior plane's Derive over the fixture (offline),
// for the harness's pin/baseline construction.
func deriveBehavior(t *testing.T, root string) plane.Derived {
	t.Helper()
	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	disc, err := manifest.Discover(root, cfg.LanguageRoots())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	refs := make([]plane.ModuleRef, 0, len(disc.Governed))
	for _, m := range disc.Governed {
		refs = append(refs, plane.ModuleRef{ID: m.ID, Path: m.Dir, Language: m.Language})
	}
	svc := plane.DeriveServices{
		RepoRoot:  root,
		Tools:     &derive.RecordedRunner{AnalysisDir: analysisDir(root)},
		ModuleOf:  disc.ModuleForFile,
		FilesOf:   disc.FilesOf,
		Languages: cfg.LanguageSpecs(),
		Commit:    "test-commit",
	}
	derived, err := behavior.New().Derive(context.Background(), refs, svc)
	if err != nil {
		t.Fatalf("behavior derive: %v", err)
	}
	return derived
}

func readSnap(t *testing.T, root, module, boundary string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(module), ".grip", "behavior", boundary+".snap"))
	if err != nil {
		t.Fatalf("read pin %s/%s: %v", module, boundary, err)
	}
	return string(b)
}

// writeBaseline records the current pin content as the git-baseline snapshot for a
// boundary, so a later re-pin is detected as an intentional change.
func writeBaseline(t *testing.T, root string, entries ...[3]string) {
	t.Helper()
	type blBoundary struct {
		Name     string `json:"name"`
		Snapshot string `json:"snapshot"`
	}
	type blModule struct {
		Module     string       `json:"module"`
		Boundaries []blBoundary `json:"boundaries"`
	}
	byMod := map[string][]blBoundary{}
	var order []string
	for _, e := range entries {
		if _, ok := byMod[e[0]]; !ok {
			order = append(order, e[0])
		}
		byMod[e[0]] = append(byMod[e[0]], blBoundary{Name: e[1], Snapshot: e[2]})
	}
	var mods []blModule
	for _, m := range order {
		mods = append(mods, blModule{Module: m, Boundaries: byMod[m]})
	}
	b, err := json.Marshal(struct {
		Modules []blModule `json:"modules"`
	}{Modules: mods})
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-baseline.json": string(b)})
}

func runGate(t *testing.T, root string) (*gate.Outcome, string) {
	t.Helper()
	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI:     true,
		Tools:  &derive.RecordedRunner{AnalysisDir: analysisDir(root)},
		Commit: "test-commit",
	})
	if err != nil {
		t.Fatalf("gate run usage error: %v", err)
	}
	return out, report.Human(report.View{Outcome: out})
}

func assertGate(t *testing.T, name string, out *gate.Outcome, human string, wantDecision string, wantExit int) {
	t.Helper()
	if out.Decision != wantDecision {
		t.Errorf("decision = %q, want %q\n%s", out.Decision, wantDecision, human)
	}
	if out.ExitCode != wantExit {
		t.Errorf("exit = %d, want %d\n%s", out.ExitCode, wantExit, human)
	}
	checkGolden(t, name, human)
}

// --- scenarios (each an exit criterion) ------------------------------------

// Exit criterion: a behavior-preserving rewrite passes (normalization strips the
// changed timestamp/address; reality still matches the pin).
func TestBehaviorPreservingRewritePasses(t *testing.T) {
	root := newRepo(t, nil)
	pin(t, root, "src/checkout", "app/Refund")
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-typescript.json": tsPreserving})
	out, human := runGate(t, root)
	assertGate(t, "behavior-preserving-passes", out, human, "pass", 0)
	if len(out.Violations) != 0 {
		t.Fatalf("expected no violations, got %s", human)
	}
}

// Exit criterion: an observable-output rewrite is blocked pending ratification.
func TestObservableChangeBlocksPendingRatify(t *testing.T) {
	root := newRepo(t, nil)
	pin(t, root, "src/checkout", "app/Refund")
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-typescript.json": tsChanged})
	out, human := runGate(t, root)
	assertGate(t, "observable-change-blocks", out, human, "block", 1)
	if !hasRuleKind(out, "behavior.unratified-change", "violation") {
		t.Fatalf("expected unratified-change violation, got %s", human)
	}
}

// Exit criterion: ratify re-pins the new behavior and the gate renders it as an
// intentional change (never a mystery violation), passing.
func TestRatifyRepinsAndRendersIntentional(t *testing.T) {
	root := newRepo(t, nil)
	pin(t, root, "src/checkout", "app/Refund")
	// Record the current (pre-change) pin as the git baseline.
	writeBaseline(t, root, [3]string{"src/checkout", "placeOrder", readSnap(t, root, "src/checkout", "placeOrder")})
	// The agent changed observable output, then ratified (re-pinned) it.
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-typescript.json": tsChanged})
	pin(t, root, "src/checkout")
	out, human := runGate(t, root)
	assertGate(t, "ratify-renders-intentional", out, human, "pass", 0)
	if !hasRuleKind(out, "behavior.unratified-change", "intentionalChange") {
		t.Fatalf("expected intentional change, got %s", human)
	}
}

// Exit criterion: a flaky/nondeterministic boundary degrades to reduced-confidence
// (fail-closed cannot-verify, exit 2), never a false pin.
func TestFlakyBoundaryDegradesToReduced(t *testing.T) {
	root := newRepo(t, nil)
	pin(t, root, "src/checkout", "app/Refund")
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-typescript.json": tsFlaky})
	out, human := runGate(t, root)
	assertGate(t, "flaky-degrades-to-reduced", out, human, "block", 2)
	if !hasRuleKind(out, "behavior.unratified-change", "cannotVerify") {
		t.Fatalf("expected cannot-verify, got %s", human)
	}
}

// Exit criterion (Tier B): a newly observed / marked-but-unpinned boundary is a
// non-blocking advisory, not a block.
func TestUnpinnedBoundaryIsAdvisory(t *testing.T) {
	root := newRepo(t, map[string]string{
		// checkout now marks `total` for pinning too; only placeOrder gets pinned.
		"src/checkout/grip.yaml":                  "module: checkout\nbehavior:\n  pin: [placeOrder, total]\n",
		".grip-analysis/behavior-typescript.json": tsWithNew,
	})
	pin(t, root, "app/Refund") // pin refund…
	pinPlaceOrderOnly(t, root) // …and only placeOrder in checkout, leaving total unpinned
	out, human := runGate(t, root)
	assertGate(t, "unpinned-boundary-advisory", out, human, "pass", 0)
	if !hasRuleKind(out, "behavior.unpinned-boundary", "violation") {
		t.Fatalf("expected unpinned-boundary advisory, got %s", human)
	}
}

// pinPlaceOrderOnly pins just checkout's placeOrder boundary (via the filtered
// SnapshotsFor path), leaving any other observed boundary unpinned.
func pinPlaceOrderOnly(t *testing.T, root string) {
	t.Helper()
	derived := deriveBehavior(t, root)
	for _, f := range behavior.SnapshotsFor(derived, "src/checkout", map[string]bool{"placeOrder": true}) {
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Bonus (fail-closed parity with M1): a missing capture helper blocks (exit 2).
func TestCaptureToolMissingFailClosed(t *testing.T) {
	root := newRepo(t, nil)
	pin(t, root, "src/checkout", "app/Refund")
	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI: true,
		Tools: &derive.RecordedRunner{
			AnalysisDir: analysisDir(root),
			Missing:     map[string]string{"behavior-typescript": "install the behavior capture helper"},
		},
		Commit: "test-commit",
	})
	if err != nil {
		t.Fatalf("gate usage error: %v", err)
	}
	if out.Decision != "block" || out.ExitCode != 2 {
		t.Fatalf("expected fail-closed block exit 2, got %s exit %d", out.Decision, out.ExitCode)
	}
	if len(out.FailClosed) == 0 {
		t.Fatal("expected a fail-closed reason")
	}
}

// TestDeterminismReport runs the drift scenario through the full gate many times
// and asserts the rendered report is byte-identical (NFR-1) for a snapshot plane.
func TestDeterminismReport(t *testing.T) {
	root := newRepo(t, nil)
	pin(t, root, "src/checkout", "app/Refund")
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-typescript.json": tsChanged})
	_, first := runGate(t, root)
	for i := 0; i < 30; i++ {
		_, human := runGate(t, root)
		if human != first {
			t.Fatalf("run %d: report drift\n--- first ---\n%s\n--- now ---\n%s", i, first, human)
		}
	}
}

// --- helpers ---------------------------------------------------------------

func hasRuleKind(out *gate.Outcome, rule, kind string) bool {
	for _, v := range out.Violations {
		if v.RuleID == rule && string(v.Kind) == kind {
			return true
		}
	}
	return false
}

func checkGolden(t *testing.T, name, human string) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	goldenPath := filepath.Join(filepath.Dir(file), "testdata", "golden", name+".report.txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(human), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if human != string(want) {
		t.Errorf("report mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, human, string(want))
	}
}
