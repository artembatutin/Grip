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

// TestGoArchitectureGate proves a compile-valid Go package dependency is
// derived from source and blocked until the source module declares it.
func TestGoArchitectureGate(t *testing.T) {
	root := t.TempDir()
	writeGoFixture(t, root, "go.mod", "module example.test/grip-go\n\ngo 1.26\n")
	writeGoFixture(t, root, ".grip.yaml", `version: 1
planes:
  architecture: { enabled: true }
languages:
  go:
    roots: ["internal"]
    tool: { name: go }
policy:
  layers: { order: [foundation, engine] }
gate:
  ci: { planes: [architecture] }
`)
	writeGoFixture(t, root, "internal/foundation/grip.yaml", `module: foundation
intent: Owns the domain value.
architecture:
  facade: [Widget]
  dependencies: { allow: [], layer: foundation }
  cycles: forbid
`)
	writeGoFixture(t, root, "internal/foundation/foundation.go", "package foundation\n\ntype Widget struct{}\n")
	writeGoFixture(t, root, "internal/engine/grip.yaml", `module: engine
intent: Owns orchestration.
architecture:
  facade: [Run]
  dependencies: { allow: [], layer: engine }
  cycles: forbid
`)
	writeGoFixture(t, root, "internal/engine/engine.go", "package engine\n\nfunc Run() {}\n")

	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	run := func() *gate.Outcome {
		out, runErr := gate.Run(context.Background(), cfg, reg, gate.Options{
			CI: true, Tools: &derive.ExecRunner{RepoRoot: root}, Commit: "go-acceptance",
		})
		if runErr != nil {
			t.Fatal(runErr)
		}
		return out
	}
	if out := run(); out.ExitCode != gate.ExitPass {
		t.Fatalf("clean Go architecture blocked: %+v", out)
	}
	t.Log("control: clean package graph passes Grip")

	writeGoFixture(t, root, "internal/engine/engine.go", `package engine

import "example.test/grip-go/internal/foundation"

func Run() { _ = foundation.Widget{} }
`)
	compile := exec.Command("go", "test", "./...")
	compile.Dir = root
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("adversarial change must compile before Grip judges it: %v\n%s", err, output)
	}
	t.Log("independent oracle: Go compiler accepts the undeclared dependency")
	out := run()
	if out.ExitCode != gate.ExitBlocked || !hasRule(out.Violations, "arch.illegal-dependency") {
		t.Fatalf("undeclared Go dependency was not blocked: %+v", out)
	}
	t.Logf("policy proof: Grip blocks the compile-valid change with %s", out.Violations[0].RuleID)

	writeGoFixture(t, root, "internal/engine/grip.yaml", `module: engine
intent: Owns orchestration.
architecture:
  facade: [Run]
  dependencies: { allow: [internal/foundation], layer: engine }
  cycles: forbid
`)
	out = run()
	if out.ExitCode != gate.ExitPass {
		t.Fatalf("explicitly ratified dependency did not pass: %+v", out)
	}
	t.Log("consent proof: the same source passes after an explicit manifest decision")
}

func writeGoFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
