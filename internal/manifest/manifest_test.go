package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tsRoots() []LanguageRoots {
	return []LanguageRoots{{Language: "typescript", Roots: []string{"src"}, Exts: []string{".ts"}}}
}

func TestDiscoveryGovernedAndUngoverned(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/domain/grip.yaml", "module: domain\n")
	write(t, root, "src/domain/index.ts", "export const x = 1;\n")
	write(t, root, "src/legacy/loader.ts", "export const y = 2;\n") // ungoverned
	write(t, root, "src/legacy/sub/deep.ts", "export const z = 3;\n")

	disc, err := Discover(root, tsRoots())
	if err != nil {
		t.Fatal(err)
	}
	if got := disc.GovernedIDs(); len(got) != 1 || got[0] != "src/domain" {
		t.Fatalf("governed = %v", got)
	}
	if got := disc.UngovernedIDs(); len(got) != 1 || got[0] != "src/legacy" {
		t.Fatalf("ungoverned = %v, want [src/legacy]", got)
	}
	if owner := disc.ModuleForFile("src/domain/index.ts"); owner != "src/domain" {
		t.Fatalf("file owner = %q", owner)
	}
	if owner := disc.ModuleForFile("src/legacy/loader.ts"); owner != "" {
		t.Fatalf("ungoverned file owner = %q, want empty", owner)
	}
}

func TestNearestAncestorGoverns(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/a/grip.yaml", "module: a\n")
	write(t, root, "src/a/b/grip.yaml", "module: b\n") // nested module
	write(t, root, "src/a/b/deep.ts", "export const q = 1;\n")
	write(t, root, "src/a/top.ts", "export const p = 1;\n")

	disc, err := Discover(root, tsRoots())
	if err != nil {
		t.Fatal(err)
	}
	if owner := disc.ModuleForFile("src/a/b/deep.ts"); owner != "src/a/b" {
		t.Fatalf("nested file owner = %q, want src/a/b", owner)
	}
	if owner := disc.ModuleForFile("src/a/top.ts"); owner != "src/a" {
		t.Fatalf("file owner = %q, want src/a", owner)
	}
}

func TestMalformedManifestFailsClosed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/a/grip.yaml", "module: [not, a, string]\n: : :\n")
	write(t, root, "src/a/x.ts", "export const x=1;\n")
	if _, err := Discover(root, tsRoots()); err == nil {
		t.Fatal("malformed manifest must fail closed")
	}
}

func TestSectionStrictDecode(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/a/grip.yaml", "module: a\narchitecture:\n  facade: [X]\n  unknownKey: true\n")
	m, err := Load(filepath.Join(root, "src/a/grip.yaml"), root)
	if err != nil {
		t.Fatal(err)
	}
	sec := m.Section("architecture")
	if !sec.Present {
		t.Fatal("architecture section should be present")
	}
	var out struct {
		Facade []string `yaml:"facade"`
	}
	if err := sec.Decode(&out); err == nil {
		t.Fatal("unknown key inside a governed section must be rejected (fail-closed)")
	}
}

func TestUnknownTopLevelKeyPreserved(t *testing.T) {
	root := t.TempDir()
	// A future plane's section must not break the loader (forward-compat).
	write(t, root, "src/a/grip.yaml", "module: a\nbehavior: { snapshot: true }\narchitecture: { facade: [X] }\n")
	m, err := Load(filepath.Join(root, "src/a/grip.yaml"), root)
	if err != nil {
		t.Fatalf("unknown top-level section must be tolerated: %v", err)
	}
	if !m.Section("behavior").Present {
		t.Fatal("future plane section should be retrievable")
	}
}
