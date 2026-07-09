package behavior

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

// stubRunner is an in-test ToolRunner that serves canned reports by tool name. A
// name in missing yields a fail-closed ToolMissingError; an absent report is an
// empty (benign) capture.
type stubRunner struct {
	reports map[string][]byte
	missing map[string]string
}

func newStubRunner() *stubRunner {
	return &stubRunner{reports: map[string][]byte{}, missing: map[string]string{}}
}

func (s *stubRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	if h, ok := s.missing[name]; ok {
		return nil, &plane.ToolMissingError{Tool: name, Hint: h}
	}
	if b, ok := s.reports[name]; ok {
		return b, nil
	}
	return []byte(`{}`), nil
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

// deriveSvc builds DeriveServices for a temp repo. The behavior plane reads pins
// from each ModuleRef.Path, so refsFor supplies those alongside the services.
func deriveSvc(root string, runner plane.ToolRunner) plane.DeriveServices {
	return plane.DeriveServices{
		RepoRoot: root,
		Tools:    runner,
		Languages: []plane.LanguageSpec{
			{Language: "typescript", Roots: []string{"src"}},
			{Language: "php", Roots: []string{"app"}},
		},
	}
}

// refsFor builds ModuleRefs for the given module ids under root, carrying the
// absolute path (where the plane reads .grip/behavior/*.snap).
func refsFor(root string, ids ...string) []plane.ModuleRef {
	refs := make([]plane.ModuleRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, plane.ModuleRef{ID: id, Path: filepath.Join(root, filepath.FromSlash(id)), Language: "typescript"})
	}
	return refs
}
