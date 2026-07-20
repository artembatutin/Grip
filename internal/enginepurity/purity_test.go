// Package enginepurity holds the engine-core-purity test (plan/02 §6, plan/03
// M0.2): it mechanically guarantees the engine names no plane. If any registered
// plane id appears as a string literal in an engine-core package, the test
// fails — which is exactly what would happen if someone wrote
// `switch plane { case "architecture": ... }` in the gate. This is what makes a
// second plane (M1) plug in without touching the engine.
package enginepurity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
)

// engineCorePackages are the packages that must be plane-agnostic. Excluded by
// design: internal/plane (the contract), internal/plane/architecture (the plane
// itself), internal/config (maps config plane keys to the registry), and
// internal/cli (the single wiring point).
var engineCorePackages = []string{
	"internal/ir",
	"internal/manifest",
	"internal/reconcile",
	"internal/gate",
	"internal/diff",
	"internal/report",
	"internal/derive",
	"internal/derive/typescript",
	"internal/derive/php",
	"internal/derive/golang",
	"internal/vcs",
	"internal/ratify",
}

func TestEngineCoreNamesNoPlane(t *testing.T) {
	repoRoot := repoRoot(t)
	planeIDs := cli.BuildRegistry().IDs()
	if len(planeIDs) == 0 {
		t.Fatal("no planes registered; purity test is vacuous")
	}
	idSet := map[string]bool{}
	for _, id := range planeIDs {
		idSet[id] = true
	}

	for _, pkg := range engineCorePackages {
		dir := filepath.Join(repoRoot, pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", pkg, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			checkFile(t, path, idSet)
		}
	}
}

// checkFile parses one Go file and fails if any string literal equals a plane id
// (comments are ignored by the AST, so a plane id in a doc comment is fine).
func checkFile(t *testing.T, path string, idSet map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val := strings.Trim(lit.Value, "`\"")
		if idSet[val] {
			pos := fset.Position(lit.Pos())
			t.Errorf("engine-core leak: plane id %q appears as a string literal at %s:%d — the engine must reach planes only through the registry",
				val, path, pos.Line)
		}
		return true
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate caller")
	}
	// internal/enginepurity/purity_test.go -> repo root is two levels up.
	return filepath.Join(filepath.Dir(file), "..", "..")
}
