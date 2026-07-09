package contract

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// section wraps a YAML fragment as a strict manifest section, exactly as the
// manifest loader hands one to a plane (KnownFields → unknown keys rejected).
func section(t *testing.T, yamlBody string) plane.ManifestSection {
	t.Helper()
	return plane.ManifestSection{
		Present: true,
		Decode: func(v interface{}) error {
			return yamlStrict(strings.NewReader(yamlBody)).Decode(v)
		},
	}
}

// stubRunner is an in-test ToolRunner that serves canned checker reports by tool
// name. A name in missing yields a fail-closed ToolMissingError; an absent report
// is an empty (benign) verdict — exactly the RecordedRunner contract.
type stubRunner struct {
	reports map[string][]byte
	missing map[string]string
}

func newStubRunner() *stubRunner {
	return &stubRunner{reports: map[string][]byte{}, missing: map[string]string{}}
}

func (s *stubRunner) with(name, body string) *stubRunner {
	s.reports[name] = []byte(body)
	return s
}

func (s *stubRunner) miss(name, hint string) *stubRunner {
	s.missing[name] = hint
	return s
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

// deriveSvc builds DeriveServices for a temp repo. The contract plane reads
// baselines from each ModuleRef.Path.
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
// absolute path (where the plane reads .grip/contract/*.contract). Language is
// typescript unless the id lives under app/ (treated as php).
func refsFor(root string, ids ...string) []plane.ModuleRef {
	refs := make([]plane.ModuleRef, 0, len(ids))
	for _, id := range ids {
		lang := "typescript"
		if strings.HasPrefix(id, "app/") {
			lang = "php"
		}
		refs = append(refs, plane.ModuleRef{ID: id, Path: filepath.Join(root, filepath.FromSlash(id)), Language: lang})
	}
	return refs
}

// writeBaseline writes a ratified baseline artifact for (module, kind).
func writeBaseline(t *testing.T, root, moduleID, kind, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(moduleID), ".grip", "contract", kind+baselineExt)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
