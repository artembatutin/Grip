package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/ir"
)

func TestGateRejectsConflictingOutputAndModeFlags(t *testing.T) {
	for _, args := range [][]string{{"gate", "--local", "--ci"}, {"gate", "--json", "--sarif"}} {
		var out, err bytes.Buffer
		a := &App{Stdout: &out, Stderr: &err, Reg: BuildRegistry()}
		if code := a.Run(args); code != exitUsage {
			t.Fatalf("Run(%v) = %d, want usage", args, code)
		}
		if !strings.Contains(err.String(), "cannot be used together") {
			t.Fatalf("Run(%v) stderr = %q", args, err.String())
		}
	}
}

func TestVersionIsMachineReadableEnoughForHumans(t *testing.T) {
	var out, err bytes.Buffer
	a := &App{Stdout: &out, Stderr: &err, Reg: BuildRegistry()}
	if code := a.Run([]string{"version"}); code != exitOK {
		t.Fatalf("version = %d, stderr=%s", code, err.String())
	}
	if !strings.Contains(out.String(), "grip ") || !strings.Contains(out.String(), "ir schema:") {
		t.Fatalf("version output = %q", out.String())
	}
}

func TestInferLanguageRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "feature", "thing.ts"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := inferLanguageRoots(root)
	if len(got) != 1 || got[0].Language != "typescript" || got[0].Roots[0] != "src" {
		t.Fatalf("roots = %#v", got)
	}
}

func TestInferGoLanguageRootUsesModuleRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "feature", "feature.go"), []byte("package feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := inferLanguageRoots(root)
	if len(got) != 1 || got[0].Language != "go" || got[0].Roots[0] != "." {
		t.Fatalf("roots = %#v", got)
	}
}

func TestSnapshotGraphKeepsShapeAndDropsEvidence(t *testing.T) {
	g := &ir.Graph{
		IRVersion: ir.Version,
		Commit:    "c1",
		Modules: []ir.Module{{
			ID: "internal/a", Language: "go", Files: []string{"internal/a/a.go"},
			Exports:              []ir.Export{{Name: "A", File: "internal/a/a.go", Line: 1}},
			ReachableFromOutside: []string{"A"},
		}},
		Edges: []ir.Edge{{
			From: "internal/b", To: "internal/a", Kind: "import",
			Evidence: []ir.Evidence{{File: "internal/b/b.go", Line: 4, Symbol: "A"}},
		}},
		Analyzers: []ir.Analyzer{{Name: "go", Version: "1.26.2", Language: "go"}},
	}
	snap := snapshotGraph(g)
	if len(snap.Modules) != 1 || len(snap.Modules[0].ReachableFromOutside) != 1 || len(snap.Edges) != 1 {
		t.Fatalf("shape missing from snapshot: %#v", snap)
	}
	if len(snap.Modules[0].Files) != 0 || len(snap.Modules[0].Exports) != 0 || len(snap.Edges[0].Evidence) != 0 || len(snap.Analyzers) != 0 {
		t.Fatalf("evidence leaked into compact snapshot: %#v", snap)
	}
}
