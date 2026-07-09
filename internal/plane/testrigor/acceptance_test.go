package testrigor_test

// This is the end-to-end M1 gate: scripted "agent" diffs over synthetic PHP+TS
// fixtures run through the REAL gate (cli.BuildRegistry → config → discovery →
// the generic plane loop → test-rigor Derive/Reconcile → decision → report),
// entirely offline via recorded Stryker/Infection output. It proves the plane
// works through the untouched engine — the plane is registered in cli and nothing
// in internal/gate|reconcile|config|ir changed — and pins the exact report
// strings via golden files. It lives in an external test package so it can import
// cli without an import cycle.

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/report"
)

var update = flag.Bool("update", false, "update golden report files")

// --- fixture repo (base) ----------------------------------------------------

const gripConfig = `version: 1
planes:
  test-rigor: { enabled: true }
languages:
  typescript:
    roots: ["src"]
    tool: { name: stryker }
  php:
    roots: ["app"]
    tool: { name: infection }
modules:
  granularity: directory
gate:
  failClosed: true
  local: { planes: [test-rigor] }
  ci: { planes: [test-rigor] }
`

// base: two governed modules (TS checkout, PHP refund), each declaring a verified
// boundary contract, with recorded reports where every contract mutant is killed
// → a clean pass. Scenarios override individual files over this base.
func baseFiles() map[string]string {
	return map[string]string{
		".grip.yaml": gripConfig,

		"src/checkout/grip.yaml":        "module: checkout\ntestRigor:\n  requiredBehaviors: [checkout]\n  mutationThreshold: 80\n  boundaryContract: true\n",
		"src/checkout/index.ts":         "export const placeOrder = () => 'ok'\n",
		"src/checkout/checkout.spec.ts": "test('places order', () => {})\n",

		"app/Refund/grip.yaml":      "module: refund\ntestRigor:\n  requiredBehaviors: [refund]\n  mutationThreshold: 75\n  boundaryContract: true\n",
		"app/Refund/Refund.php":     "<?php class Refund {}\n",
		"app/Refund/RefundTest.php": "<?php class RefundTest {}\n",

		".grip-analysis/testrigor-typescript.json": tsClean,
		".grip-analysis/testrigor-php.json":        phpClean,
	}
}

const tsClean = `{"tool":{"name":"stryker","version":"8.2.0"},"modules":[
  {"module":"src/checkout","coverage":95,"mockRatio":15,"tests":[
    {"id":"checkout.spec.ts::places order","behaviors":["checkout"],"contract":true,"mutantsInScope":8,"mutantsKilled":8,"file":"src/checkout/checkout.spec.ts","line":1}
  ]}]}`

const phpClean = `{"tool":{"name":"infection","version":"0.29.0"},"modules":[
  {"module":"app/Refund","coverage":88,"mockRatio":10,"tests":[
    {"id":"RefundTest::testRefundsOrder","behaviors":["refund"],"contract":true,"mutantsInScope":6,"mutantsKilled":6,"file":"app/Refund/RefundTest.php","line":10}
  ]}]}`

// TS report where the checkout boundary-contract test kills NO mutants (vacuous).
const tsVacuous = `{"tool":{"name":"stryker","version":"8.2.0"},"modules":[
  {"module":"src/checkout","coverage":95,"mockRatio":15,"tests":[
    {"id":"checkout.spec.ts::places order","behaviors":["checkout"],"contract":true,"mutantsInScope":8,"mutantsKilled":0,"file":"src/checkout/checkout.spec.ts","line":1}
  ]}]}`

// PHP report where the refund boundary-contract test kills NO mutants (vacuous).
const phpVacuous = `{"tool":{"name":"infection","version":"0.29.0"},"modules":[
  {"module":"app/Refund","coverage":88,"mockRatio":10,"tests":[
    {"id":"RefundTest::testRefundsOrder","behaviors":["refund"],"contract":true,"mutantsInScope":6,"mutantsKilled":0,"file":"app/Refund/RefundTest.php","line":10}
  ]}]}`

// TS report where the checkout contract test is flaky (untrustworthy kill signal).
const tsFlakyContract = `{"tool":{"name":"stryker","version":"8.2.0"},"modules":[
  {"module":"src/checkout","coverage":95,"mockRatio":15,"tests":[
    {"id":"checkout.spec.ts::places order","behaviors":[],"contract":true,"flaky":true,"mutantsInScope":8,"mutantsKilled":8,"file":"src/checkout/checkout.spec.ts","line":1}
  ]}]}`

// TS report where behavior "audit-log" is covered only by a skipped test (the
// contract test still kills its mutants, so only skipped-required-test fires).
const tsSkipped = `{"tool":{"name":"stryker","version":"8.2.0"},"modules":[
  {"module":"src/checkout","coverage":95,"mockRatio":15,"tests":[
    {"id":"checkout.spec.ts::places order","behaviors":["checkout"],"contract":true,"mutantsInScope":8,"mutantsKilled":8,"file":"src/checkout/checkout.spec.ts","line":1},
    {"id":"checkout.spec.ts::writes audit log","behaviors":["audit-log"],"skipped":true,"mutantsInScope":0,"mutantsKilled":0,"file":"src/checkout/checkout.spec.ts","line":9}
  ]}]}`

// baseline snapshot: prior-commit thresholds + required-test set, used by the
// deleted-required-test and threshold-tamper scenarios.
const baselineTamperDeleted = `{"modules":[
  {"module":"src/checkout","threshold":80,"mutationScore":90,"mockRatio":15,
   "requiredTests":{"checkout":["checkout.spec.ts::places order","checkout.spec.ts::rejects empty cart"]}}
]}`

// --- scenario harness -------------------------------------------------------

type scenario struct {
	name         string
	overrides    map[string]string
	wantDecision string
	wantExit     int
	wantRule     string
	wantNotRule  string
	wantContains []string
	golden       bool // pin the exact human report
}

func runScenario(t *testing.T, sc scenario) (*gate.Outcome, string) {
	t.Helper()
	root := t.TempDir()
	files := baseFiles()
	for k, v := range sc.overrides {
		if v == "" {
			delete(files, k)
			continue
		}
		files[k] = v
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI:     true,
		Tools:  &derive.RecordedRunner{AnalysisDir: filepath.Join(root, ".grip-analysis")},
		Commit: "test-commit",
	})
	if err != nil {
		t.Fatalf("gate run usage error: %v", err)
	}
	return out, report.Human(report.View{Outcome: out})
}

func TestAcceptanceMatrix(t *testing.T) {
	scenarios := []scenario{
		{
			name:         "clean-base-passes",
			wantDecision: "pass",
			wantExit:     0,
			wantNotRule:  "test.vacuous-contract",
			wantContains: []string{"PASS", "planes: test-rigor · governed modules: 2"},
			golden:       true,
		},
		{
			name:         "ts-vacuous-contract-blocks",
			overrides:    map[string]string{".grip-analysis/testrigor-typescript.json": tsVacuous},
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "test.vacuous-contract",
			wantContains: []string{"src/checkout", "kills none of its 8 mutants"},
			golden:       true,
		},
		{
			name:         "php-vacuous-contract-blocks",
			overrides:    map[string]string{".grip-analysis/testrigor-php.json": phpVacuous},
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "test.vacuous-contract",
			wantContains: []string{"app/Refund", "kills none of its 6 mutants"},
			golden:       true,
		},
		{
			name: "deleted-required-test-blocks",
			overrides: map[string]string{
				".grip-analysis/testrigor-baseline.json": baselineTamperDeleted,
			},
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "test.deleted-required-test",
			wantContains: []string{"no longer has a test for required behavior \"checkout\"", "rejects empty cart"},
			golden:       true,
		},
		{
			name: "skipped-required-test-blocks",
			overrides: map[string]string{
				"src/checkout/grip.yaml":                   "module: checkout\ntestRigor:\n  requiredBehaviors: [checkout, audit-log]\n  mutationThreshold: 80\n  boundaryContract: true\n",
				".grip-analysis/testrigor-typescript.json": tsSkipped,
			},
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "test.skipped-required-test",
			wantContains: []string{"required behavior \"audit-log\" is verified only by skipped test"},
			golden:       true,
		},
		{
			name: "threshold-tamper-blocks",
			overrides: map[string]string{
				"src/checkout/grip.yaml":                 "module: checkout\ntestRigor:\n  requiredBehaviors: [checkout]\n  mutationThreshold: 50\n  boundaryContract: true\n",
				".grip-analysis/testrigor-baseline.json": `{"modules":[{"module":"src/checkout","threshold":80,"requiredTests":{"checkout":["checkout.spec.ts::places order"]}}]}`,
			},
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "test.threshold-tamper",
			wantContains: []string{"lowered its mutationThreshold from 80 to 50"},
			golden:       true,
		},
		{
			name:         "flaky-contract-fail-closed",
			overrides:    map[string]string{".grip-analysis/testrigor-typescript.json": tsFlakyContract},
			wantDecision: "block",
			wantExit:     2, // fail-closed: a flaky signal must never pass silently
			wantContains: []string{"cannot verify", "is flaky"},
			golden:       true,
		},
		{
			name:         "tool-missing-fail-closed",
			overrides:    map[string]string{".grip-analysis/testrigor-php.json": ""}, // delete → recorded runner still returns empty; force missing below
			wantDecision: "block",
			wantExit:     2,
			wantContains: []string{"tool-missing"},
		},
		{
			name: "unverified-module-reported-not-blocking",
			overrides: map[string]string{
				"src/checkout/grip.yaml": "module: checkout\ntestRigor:\n  requiredBehaviors: [checkout]\n  mutationThreshold: 80\n",
			},
			wantDecision: "pass",
			wantExit:     0,
			wantRule:     "test.unverified-module",
			wantContains: []string{"no verified boundary contract"},
			golden:       true,
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			var out *gate.Outcome
			var human string
			if sc.name == "tool-missing-fail-closed" {
				out, human = runToolMissing(t)
			} else {
				out, human = runScenario(t, sc)
			}
			if out.Decision != sc.wantDecision {
				t.Errorf("decision = %q, want %q\n%s", out.Decision, sc.wantDecision, human)
			}
			if out.ExitCode != sc.wantExit {
				t.Errorf("exit = %d, want %d\n%s", out.ExitCode, sc.wantExit, human)
			}
			if sc.wantRule != "" && !hasRule(out.Violations, sc.wantRule) {
				t.Errorf("expected rule %q not fired; got %s\n%s", sc.wantRule, ruleList(out.Violations), human)
			}
			if sc.wantNotRule != "" && hasRule(out.Violations, sc.wantNotRule) {
				t.Errorf("rule %q fired but must not\n%s", sc.wantNotRule, human)
			}
			for _, want := range sc.wantContains {
				if !strings.Contains(human, want) {
					t.Errorf("report missing %q\n%s", want, human)
				}
			}
			if sc.golden {
				checkGolden(t, sc.name, human)
			}
		})
	}
}

// runToolMissing exercises the fail-closed path when a mutation tool for an
// enabled language is not installed (an opted-in PHP module + missing Infection).
func runToolMissing(t *testing.T) (*gate.Outcome, string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range baseFiles() {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI: true,
		Tools: &derive.RecordedRunner{
			AnalysisDir: filepath.Join(root, ".grip-analysis"),
			Missing:     map[string]string{"testrigor-php": "install PHP + infection"},
		},
		Commit: "test-commit",
	})
	if err != nil {
		t.Fatalf("gate run usage error: %v", err)
	}
	return out, report.Human(report.View{Outcome: out})
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

// TestDeterminismReport runs a multi-violation scenario (both languages vacuous)
// through the full gate many times and asserts the rendered report is
// byte-identical every run — the value the determinism CI matrix asserts (NFR-1),
// now proven for a NON-graph plane. Any map-iteration-order leak in Derive or
// Reconcile would surface here.
func TestDeterminismReport(t *testing.T) {
	sc := scenario{
		name: "both-vacuous",
		overrides: map[string]string{
			".grip-analysis/testrigor-typescript.json": tsVacuous,
			".grip-analysis/testrigor-php.json":        phpVacuous,
		},
	}
	_, first := runScenario(t, sc)
	if !strings.Contains(first, "src/checkout") || !strings.Contains(first, "app/Refund") {
		t.Fatalf("expected both modules to violate:\n%s", first)
	}
	for i := 0; i < 50; i++ {
		out, human := runScenario(t, sc)
		if human != first {
			t.Fatalf("run %d: report drift\n--- first ---\n%s\n--- now ---\n%s", i, first, human)
		}
		if out.Decision != "block" || out.ExitCode != 1 {
			t.Fatalf("run %d: decision drift: %s exit %d", i, out.Decision, out.ExitCode)
		}
	}
}

func hasRule(vs []plane.Violation, id string) bool {
	for _, v := range vs {
		if v.RuleID == id {
			return true
		}
	}
	return false
}

func ruleList(vs []plane.Violation) string {
	var ids []string
	for _, v := range vs {
		ids = append(ids, v.RuleID+"/"+string(v.Kind))
	}
	return strings.Join(ids, ", ")
}
