package acceptance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
)

func TestGoBehaviorPlaneBlocksCorrelatedCodeAndTestChange(t *testing.T) {
	root := newGoPlaneRepo(t, "behavior")
	writeGoFixture(t, root, "pkg/grip.yaml", `module: pkg
intent: Exposes one observable value.
behavior: { pin: [ExampleValue] }
`)
	writeGoFixture(t, root, "pkg/value.go", "package pkg\n\nfunc Value() string { return \"old\" }\n")
	writeGoFixture(t, root, "pkg/value_test.go", `package pkg
import "fmt"
func ExampleValue() {
  fmt.Println(Value())
  // Output: old
}
`)
	writeGoFixture(t, root, "pkg/.grip/behavior/ExampleValue.snap", "grip:behavior/v1\nmodule: pkg\nboundary: ExampleValue\n---\n- example: old\n")

	if out := runGoPlaneGate(t, root, "behavior"); out.ExitCode != gate.ExitPass {
		t.Fatalf("clean behavior blocked: %+v", out)
	}
	writeGoFixture(t, root, "pkg/value.go", "package pkg\n\nfunc Value() string { return \"new\" }\n")
	writeGoFixture(t, root, "pkg/value_test.go", `package pkg
import "fmt"
func ExampleValue() {
  fmt.Println(Value())
  // Output: new
}
`)
	out := runGoPlaneGate(t, root, "behavior")
	if out.ExitCode != gate.ExitBlocked || !hasRule(out.Violations, "behavior.unratified-change") {
		t.Fatalf("correlated behavior rewrite was not blocked: %+v", out)
	}
	t.Log("Go tests accept the coordinated code+expectation rewrite; the pinned behavior plane blocks it")

	writeGoFixture(t, root, "pkg/.grip/behavior/ExampleValue.snap", "grip:behavior/v1\nmodule: pkg\nboundary: ExampleValue\n---\n- example: new\n")
	if out := runGoPlaneGate(t, root, "behavior"); out.ExitCode != gate.ExitPass {
		t.Fatalf("re-pinned behavior did not pass: %+v", out)
	}
}

func TestGoContractPlaneBlocksSignatureBreak(t *testing.T) {
	root := newGoPlaneRepo(t, "contract")
	writeGoFixture(t, root, "pkg/grip.yaml", `module: pkg
intent: Exposes one API.
contract:
  api: { compat: backward }
`)
	writeGoFixture(t, root, "pkg/value.go", "package pkg\n\nfunc Value() string { return \"ok\" }\n")
	writeGoFixture(t, root, "pkg/.grip/contract/api.contract", "grip:contract/go-api/v1\nValue\tfunc\tfunc() string\n")
	if out := runGoPlaneGate(t, root, "contract"); out.ExitCode != gate.ExitPass {
		t.Fatalf("clean API contract blocked: %+v", out)
	}

	writeGoFixture(t, root, "pkg/value.go", "package pkg\n\nfunc Value(prefix string) string { return prefix + \"ok\" }\n")
	out := runGoPlaneGate(t, root, "contract")
	if out.ExitCode != gate.ExitBlocked || !hasRule(out.Violations, "contract.breaking-api") {
		t.Fatalf("breaking Go signature was not blocked: %+v", out)
	}
	t.Log("compile-valid exported signature change is blocked against the ratified Go API")

	writeGoFixture(t, root, "pkg/.grip/contract/api.contract", "grip:contract/go-api/v1\nValue\tfunc\tfunc(prefix string) string\n")
	if out := runGoPlaneGate(t, root, "contract"); out.ExitCode != gate.ExitPass {
		t.Fatalf("re-ratified API did not pass: %+v", out)
	}
}

func TestGoTestRigorPlaneBlocksVacuousBoundaryTest(t *testing.T) {
	root := newGoPlaneRepo(t, "test-rigor")
	writeGoFixture(t, root, "pkg/grip.yaml", `module: pkg
intent: Classifies positive numbers.
testRigor:
  requiredBehaviors: [positive-classification]
  boundaryContract: true
`)
	writeGoFixture(t, root, "pkg/value.go", "package pkg\n\nfunc Positive(n int) bool { return n > 0 }\n")
	writeGoFixture(t, root, "pkg/value_test.go", `package pkg
import "testing"
// grip:test behavior=positive-classification contract
func TestPositive(t *testing.T) {
  if !Positive(1) || Positive(0) { t.Fatal("wrong classification") }
}
`)
	if out := runGoPlaneGate(t, root, "test-rigor"); out.ExitCode != gate.ExitPass {
		t.Fatalf("meaningful mutation-killing test blocked: %+v", out)
	}

	writeGoFixture(t, root, "pkg/value_test.go", `package pkg
import "testing"
// grip:test behavior=positive-classification contract
func TestPositive(t *testing.T) {
  _ = Positive(1)
}
`)
	out := runGoPlaneGate(t, root, "test-rigor")
	if out.ExitCode != gate.ExitBlocked || !hasRule(out.Violations, "test.vacuous-contract") {
		t.Fatalf("vacuous Go boundary test was not blocked: %+v", out)
	}
	t.Log("ordinary Go tests remain green, but real source mutation proves the boundary assertion is vacuous")

	writeGoFixture(t, root, "pkg/value_test.go", `package pkg
import "testing"
// grip:test behavior=positive-classification contract
func TestPositive(t *testing.T) {
  t.Skip("temporarily disabled")
}
`)
	out = runGoPlaneGate(t, root, "test-rigor")
	if out.ExitCode != gate.ExitBlocked || !hasRule(out.Violations, "test.skipped-required-test") {
		t.Fatalf("skipped required Go test was not blocked: %+v", out)
	}
}

func TestGoTestRigorPlaneUsesGitBaselineForDeletionAndThreshold(t *testing.T) {
	root := newGoPlaneRepo(t, "test-rigor")
	writeGoFixture(t, root, "pkg/grip.yaml", `module: pkg
intent: Classifies positive numbers.
testRigor:
  requiredBehaviors: [positive-classification]
  mutationThreshold: 80
  boundaryContract: true
`)
	writeGoFixture(t, root, "pkg/value.go", "package pkg\n\nfunc Positive(n int) bool { return n > 0 }\n")
	writeGoFixture(t, root, "pkg/value_test.go", `package pkg
import "testing"
// grip:test behavior=positive-classification contract
func TestPositive(t *testing.T) {
  if !Positive(1) || Positive(0) { t.Fatal("wrong classification") }
}
`)
	gitCommand(t, root, "init")
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "-c", "user.name=Grip", "-c", "user.email=grip@example.test", "commit", "-m", "baseline")
	if out := runGoPlaneGate(t, root, "test-rigor"); out.ExitCode != gate.ExitPass {
		t.Fatalf("committed baseline blocked: %+v", out)
	}

	if err := os.Remove(filepath.Join(root, "pkg", "value_test.go")); err != nil {
		t.Fatal(err)
	}
	writeGoFixture(t, root, "pkg/grip.yaml", `module: pkg
intent: Classifies positive numbers.
testRigor:
  requiredBehaviors: [positive-classification]
  mutationThreshold: 50
  boundaryContract: true
`)
	out := runGoPlaneGate(t, root, "test-rigor")
	if out.ExitCode != gate.ExitBlocked || !hasRule(out.Violations, "test.deleted-required-test") || !hasRule(out.Violations, "test.threshold-tamper") {
		t.Fatalf("Git-backed deletion/threshold tamper was not blocked: %+v", out)
	}
	t.Log("prior-commit evidence blocks both deleting a required Go test and lowering its mutation threshold")
}

func newGoPlaneRepo(t *testing.T, planeID string) string {
	t.Helper()
	root := t.TempDir()
	writeGoFixture(t, root, "go.mod", "module example.test/planes\n\ngo 1.26\n")
	writeGoFixture(t, root, ".grip.yaml", `version: 1
planes:
  `+planeID+`: { enabled: true }
languages:
  go:
    roots: ["pkg"]
    tool: { name: go }
gate:
  ci: { planes: [`+planeID+`] }
`)
	return root
}

func runGoPlaneGate(t *testing.T, root, planeID string) *gate.Outcome {
	t.Helper()
	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI: true, Planes: []string{planeID}, Tools: &derive.ExecRunner{RepoRoot: root}, Commit: "go-plane-proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func gitCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
