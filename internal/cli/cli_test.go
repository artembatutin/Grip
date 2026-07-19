package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
