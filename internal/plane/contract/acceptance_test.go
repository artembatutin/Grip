package contract_test

// This is the end-to-end M3 gate: scripted "agent" diffs over synthetic PHP+TS
// fixtures run through the REAL gate (cli.BuildRegistry → config → discovery → the
// generic plane loop → contract Derive/Reconcile → decision → report), entirely
// offline via recorded checker reports and git-tracked baseline artifacts. It
// proves the plane works through the UNTOUCHED engine — contract is registered in
// cli and nothing in internal/gate|reconcile|config|ir changed — and pins the
// exact report strings via golden files. It lives in an external test package so
// it can import cli without an import cycle.

import (
	"context"
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
	"github.com/artembatutin/grip/internal/plane/contract"
	"github.com/artembatutin/grip/internal/report"
)

var update = flag.Bool("update", false, "update golden report files")

// --- fixture repo (base) ----------------------------------------------------

const gripConfig = `version: 1
planes:
  contract: { enabled: true }
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
  local: { planes: [contract] }
  ci: { planes: [contract] }
`

// base: a TS module (checkout) governing its api under backward compat, and a PHP
// module (orders) governing both its events and db schema. The base checker reports
// carry a currentShape and no changes, so adopting them yields a clean pass by
// construction; scenarios then swap in an agent's change.
func baseFiles() map[string]string {
	return map[string]string{
		".grip.yaml": gripConfig,

		"src/checkout/grip.yaml": "module: checkout\ncontract:\n  api: { compat: backward }\n",
		"src/checkout/index.ts":  "export const routes = () => 'ok'\n",

		"app/Orders/grip.yaml":  "module: orders\ncontract:\n  events: { compat: backward }\n  db: { compat: backward }\n",
		"app/Orders/Orders.php": "<?php class Orders {}\n",

		".grip-analysis/contract-api-typescript.json": apiBase,
		".grip-analysis/contract-events.json":         eventsBase,
		".grip-analysis/contract-db.json":             dbBase,
	}
}

// Base reports: resolved, a currentShape to adopt, and no changes.
const apiBase = `{"tool":{"name":"grip-contract-api-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi: checkout v1","changes":[]}]}`

const eventsBase = `{"tool":{"name":"grip-contract-events","version":"1.0.0"},"modules":[
  {"module":"app/Orders","resolved":true,"currentShape":"OrderPlaced v1","changes":[]}]}`

const dbBase = `{"tool":{"name":"grip-contract-db","version":"1.0.0"},"modules":[
  {"module":"app/Orders","resolved":true,"currentShape":"schema v1","changes":[]}]}`

// apiRemoved: the agent removed an in-use response field billing depends on.
const apiRemoved = `{"tool":{"name":"grip-contract-api-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi: checkout v2","changes":[
    {"nature":"removed","element":"GET /orders#total","consumer":"billing","file":"src/checkout/routes.ts","line":12}]}]}`

// apiRenamed: the agent renamed an in-use request field.
const apiRenamed = `{"tool":{"name":"grip-contract-api-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi: checkout v2","changes":[
    {"nature":"renamed","element":"coupon → couponCode","consumer":"mobile-app","file":"src/checkout/routes.ts","line":20}]}]}`

// apiAdditive: the agent added an optional field — additive (Tier B), never a block.
const apiAdditive = `{"tool":{"name":"grip-contract-api-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi: checkout v2","changes":[
    {"nature":"optional-added","element":"GET /orders#nickname","file":"src/checkout/routes.ts","line":30}]}]}`

// apiWidened: the agent loosened a type — compatible under backward, passes clean.
const apiWidened = `{"tool":{"name":"grip-contract-api-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi: checkout v2","changes":[
    {"nature":"widened","element":"GET /orders#status","file":"src/checkout/routes.ts","line":8}]}]}`

// apiRatified: post-`grip ratify contract` — current now matches the new baseline.
const apiRatified = `{"tool":{"name":"grip-contract-api-ts","version":"1.0.0"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi: checkout v2","changes":[]}]}`

// eventsRemoved: the agent dropped a field from a published message.
const eventsRemoved = `{"tool":{"name":"grip-contract-events","version":"1.0.0"},"modules":[
  {"module":"app/Orders","resolved":true,"currentShape":"OrderPlaced v2","changes":[
    {"nature":"removed","element":"OrderPlaced.total","consumer":"analytics","file":"app/Orders/events/OrderPlaced.json","line":5}]}]}`

// dbDestructive: the agent added a migration that drops an in-use column.
const dbDestructive = `{"tool":{"name":"grip-contract-db","version":"1.0.0"},"modules":[
  {"module":"app/Orders","resolved":true,"currentShape":"schema v2","changes":[
    {"nature":"destructive","element":"orders.total","detail":"DROP COLUMN","file":"app/Orders/migrations/0002_drop_total.sql","line":1}]}]}`

// dbUnresolved: the checker could not resolve the prior schema state → fail-closed.
const dbUnresolved = `{"tool":{"name":"grip-contract-db","version":"1.0.0"},"modules":[
  {"module":"app/Orders","resolved":false,"reason":"prior migration 0001 is unreadable in this checkout"}]}`

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

// adopt captures the current derived contracts and writes the baseline artifacts
// for a module's kinds — exactly what `grip ratify contract` does. This establishes
// the "already ratified" baseline before simulating an agent's change.
func adopt(t *testing.T, root, module string, kinds ...string) {
	t.Helper()
	derived := deriveContract(t, root)
	filter := map[string]bool{}
	for _, k := range kinds {
		filter[k] = true
	}
	for _, f := range contract.BaselinesFor(derived, module, filter) {
		if f.Missing {
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

// deriveContract runs just the contract plane's Derive over the fixture (offline),
// for the harness's adopt/baseline construction.
func deriveContract(t *testing.T, root string) plane.Derived {
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
	derived, err := contract.New().Derive(context.Background(), refs, svc)
	if err != nil {
		t.Fatalf("contract derive: %v", err)
	}
	return derived
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

func assertGate(t *testing.T, name string, out *gate.Outcome, human, wantDecision string, wantExit int) {
	t.Helper()
	if out.Decision != wantDecision {
		t.Errorf("decision = %q, want %q\n%s", out.Decision, wantDecision, human)
	}
	if out.ExitCode != wantExit {
		t.Errorf("exit = %d, want %d\n%s", out.ExitCode, wantExit, human)
	}
	checkGolden(t, name, human)
}

// adoptBase brings the base fixture to a clean ratified state.
func adoptBase(t *testing.T, root string) {
	t.Helper()
	adopt(t, root, "src/checkout", contract.KindAPI)
	adopt(t, root, "app/Orders", contract.KindEvents, contract.KindDB)
}

// --- scenarios (each an exit criterion) ------------------------------------

// Exit criterion: the adopted base is a clean pass (all kinds resolved, no change).
func TestCleanBasePasses(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	out, human := runGate(t, root)
	assertGate(t, "clean-base-passes", out, human, "pass", 0)
	if len(out.Violations) != 0 {
		t.Fatalf("expected no violations, got %s", human)
	}
}

// Exit criterion: a removed in-use API field blocks, naming what broke and who.
func TestRemovedApiFieldBlocks(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiRemoved})
	out, human := runGate(t, root)
	assertGate(t, "api-removed-field-blocks", out, human, "block", 1)
	if !hasRuleKind(out, "contract.breaking-api", "violation") {
		t.Fatalf("expected breaking-api violation, got %s", human)
	}
}

// Exit criterion (renamed variant of the same rule): a renamed in-use field blocks.
func TestRenamedApiFieldBlocks(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiRenamed})
	out, human := runGate(t, root)
	assertGate(t, "api-renamed-field-blocks", out, human, "block", 1)
}

// Exit criterion: an incompatible (destructive) migration blocks.
func TestDestructiveMigrationBlocks(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-db.json": dbDestructive})
	out, human := runGate(t, root)
	assertGate(t, "db-destructive-migration-blocks", out, human, "block", 1)
	if !hasRuleKind(out, "contract.breaking-db", "violation") {
		t.Fatalf("expected breaking-db violation, got %s", human)
	}
}

// Exit criterion: an event-shape break blocks.
func TestEventShapeBreakBlocks(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-events.json": eventsRemoved})
	out, human := runGate(t, root)
	assertGate(t, "event-shape-break-blocks", out, human, "block", 1)
	if !hasRuleKind(out, "contract.breaking-event", "violation") {
		t.Fatalf("expected breaking-event violation, got %s", human)
	}
}

// Exit criterion (Tier B): an additive field warns but does not block.
func TestAdditiveFieldWarns(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiAdditive})
	out, human := runGate(t, root)
	assertGate(t, "api-additive-field-warns", out, human, "pass", 0)
	if !hasRuleKind(out, "contract.additive-api", "violation") {
		t.Fatalf("expected additive-api advisory, got %s", human)
	}
}

// Exit criterion (near-miss for breaking-api): a compatible (widened) change passes
// clean — no block, no advisory.
func TestCompatibleChangePasses(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiWidened})
	out, human := runGate(t, root)
	assertGate(t, "compatible-change-passes", out, human, "pass", 0)
	if len(out.Violations) != 0 {
		t.Fatalf("expected no violations for a compatible change, got %s", human)
	}
}

// Exit criterion: adopt-current-as-baseline via ratify renders the change as
// intentional (never a mystery violation), passing.
func TestRatifyRendersIntentional(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	// Record the base api contract as the git-baseline (prior commit) shape.
	writeFiles(t, root, map[string]string{
		".grip-analysis/contract-baseline.json": `{"modules":[{"module":"src/checkout","kinds":[{"kind":"api","baseline":"openapi: checkout v1"}]}]}`,
	})
	// The agent changed the API (removed a field), then ratified it: adopt the new
	// contract and swap in the post-ratify checker verdict (current == new baseline).
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiRemoved})
	adopt(t, root, "src/checkout", contract.KindAPI)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiRatified})
	out, human := runGate(t, root)
	assertGate(t, "ratify-renders-intentional", out, human, "pass", 0)
	if !hasRuleKind(out, "contract.breaking-api", "intentionalChange") {
		t.Fatalf("expected intentional change, got %s", human)
	}
}

// Exit criterion: an unresolvable prior version touching a rule fails closed
// (cannot-verify, exit 2) — never a false pass.
func TestUnresolvablePriorFailsClosed(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-db.json": dbUnresolved})
	out, human := runGate(t, root)
	assertGate(t, "unresolvable-prior-fail-closed", out, human, "block", 2)
	if !hasRuleKind(out, "contract.breaking-db", "cannotVerify") {
		t.Fatalf("expected cannot-verify, got %s", human)
	}
}

// Bonus (fail-closed parity with M1/M2): a missing checker blocks (exit 2).
func TestCheckerToolMissingFailClosed(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI: true,
		Tools: &derive.RecordedRunner{
			AnalysisDir: analysisDir(root),
			Missing:     map[string]string{"contract-api-typescript": "install the api contract checker"},
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
// and asserts the rendered report is byte-identical (NFR-1).
func TestDeterminismReport(t *testing.T) {
	root := newRepo(t, nil)
	adoptBase(t, root)
	writeFiles(t, root, map[string]string{".grip-analysis/contract-db.json": dbDestructive})
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
