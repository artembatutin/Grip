// Package manifest loads per-module grip.yaml files and discovers governed vs
// ungoverned modules (plan/01 §5, plan/03 M0.1). A directory containing a
// grip.yaml is a governed module; its id is its repo-relative path (D4).
//
// The loader validates the generic envelope (module, intent) but hands each
// plane's own section to that plane for strict decoding — the engine never
// interprets a plane's schema. Unknown top-level keys are preserved untouched
// for forward-compatibility with later planes; unknown keys inside a governed
// section are the plane's business and rejected there (fail-closed).
package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/artembatutin/grip/internal/plane"
	"gopkg.in/yaml.v3"
)

// Filename is the fixed name that marks a directory as a governed module.
const Filename = "grip.yaml"

// Manifest is one module's parsed declaration.
type Manifest struct {
	// Module is the human-facing name (id is the directory path).
	Module string
	// Intent is the free-text single-responsibility statement (advisory, not
	// gated). Anything requiring paragraphs belongs here, not in a rule.
	Intent string
	// ID is the repo-relative module directory (the graph node id).
	ID string
	// Dir is the absolute module directory.
	Dir string
	// Path is the absolute path of the grip.yaml.
	Path string

	sections map[string]*yaml.Node
}

// envelope captures the plane-agnostic keys with known types.
type envelope struct {
	Module string `yaml:"module"`
	Intent string `yaml:"intent"`
}

var reservedKeys = map[string]bool{"module": true, "intent": true}

// Load reads and parses a grip.yaml at path. repoRoot is used to compute the
// repo-relative module id. A malformed manifest is a fail-closed error with a
// precise message (exit 3 at the CLI boundary).
func Load(path, repoRoot string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, fmt.Errorf("manifest %s is empty", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("manifest %s must be a YAML mapping at the top level", path)
	}

	var env envelope
	if err := root.Decode(&env); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	id, err := relID(repoRoot, dir)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		Module:   env.Module,
		Intent:   env.Intent,
		ID:       id,
		Dir:      dir,
		Path:     path,
		sections: map[string]*yaml.Node{},
	}
	// Bucket every non-reserved top-level key as a plane section, keyed by name.
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		val := root.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("manifest %s: non-scalar top-level key", path)
		}
		if reservedKeys[key.Value] {
			continue
		}
		m.sections[key.Value] = val
	}
	return m, nil
}

// Section returns the raw YAML section a plane owns, wrapped so the plane can
// strictly decode it (unknown fields rejected). Present is false when the module
// declares nothing for that plane.
func (m *Manifest) Section(name string) plane.ManifestSection {
	node, ok := m.sections[name]
	if !ok {
		return plane.ManifestSection{Present: false}
	}
	return plane.ManifestSection{
		Present: true,
		Decode: func(v interface{}) error {
			// Re-marshal the node and decode strictly: yaml.Node.Decode does not
			// itself reject unknown fields, so we route through a KnownFields
			// decoder to make a typo inside a governed section fail closed.
			b, err := yaml.Marshal(node)
			if err != nil {
				return fmt.Errorf("re-encode %q section of %s: %w", name, m.Path, err)
			}
			dec := yaml.NewDecoder(bytes.NewReader(b))
			dec.KnownFields(true)
			if err := dec.Decode(v); err != nil {
				return fmt.Errorf("%q section of %s: %w", name, m.Path, err)
			}
			return nil
		},
	}
}

// relID returns the repo-relative, slash-separated id for an absolute dir.
func relID(repoRoot, dir string) (string, error) {
	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		return "", fmt.Errorf("locate %s under repo root %s: %w", dir, repoRoot, err)
	}
	return filepath.ToSlash(rel), nil
}
