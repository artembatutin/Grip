package testrigor

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
	"gopkg.in/yaml.v3"
)

// yamlStrict mirrors the manifest loader's strict decode (unknown fields
// rejected), so intent tests exercise the same fail-closed behavior production
// gets from manifest.Section.
func yamlStrict(r io.Reader) *yaml.Decoder {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	return dec
}

// stubRunner is an in-test ToolRunner that serves canned reports by tool name and
// counts Run calls per name (so a test can prove the cache skipped a mutation
// run). A name in missing yields a fail-closed ToolMissingError.
type stubRunner struct {
	reports map[string][]byte
	missing map[string]string
	runs    map[string]int
}

func newStubRunner() *stubRunner {
	return &stubRunner{reports: map[string][]byte{}, missing: map[string]string{}, runs: map[string]int{}}
}

func (s *stubRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	s.runs[name]++
	if h, ok := s.missing[name]; ok {
		return nil, &plane.ToolMissingError{Tool: name, Hint: h}
	}
	if b, ok := s.reports[name]; ok {
		return b, nil
	}
	return []byte(`{}`), nil // absent report → empty (benign)
}

func (s *stubRunner) Version(ctx context.Context, name string) (string, error) { return "v1", nil }

// writeRepo materializes files (repo-relative path -> content) under a fresh temp
// dir and returns its root.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// overwrite replaces a file's content in an existing temp repo (used to change a
// module's content hash and prove the cache re-runs).
func overwrite(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// deriveSvc builds DeriveServices for a temp repo where each module owns the files
// listed in filesByModule.
func deriveSvc(root string, runner plane.ToolRunner, filesByModule map[string][]string) plane.DeriveServices {
	fileOwner := map[string]string{}
	for id, fs := range filesByModule {
		for _, f := range fs {
			fileOwner[f] = id
		}
	}
	return plane.DeriveServices{
		RepoRoot: root,
		Tools:    runner,
		FilesOf:  func(id string) []string { return filesByModule[id] },
		ModuleOf: func(f string) string { return fileOwner[f] },
		Languages: []plane.LanguageSpec{
			{Language: "typescript", Roots: []string{"src"}},
			{Language: "php", Roots: []string{"app"}},
		},
	}
}
