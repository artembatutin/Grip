package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Merge folds one or more per-language graphs into a single canonical graph.
// This is what makes the IR genuinely multi-language (D2): the TS deriver and
// the PHP deriver each emit a Graph, and the orchestrator merges them into one
// model with no engine-level branching on language. Modules are keyed by id;
// two derivers must never claim the same module id (they cover disjoint roots),
// so a collision is a programmer/config error and is reported, not silently
// merged.
func Merge(commit string, graphs ...*Graph) (*Graph, error) {
	out := &Graph{IRVersion: Version, Commit: commit}
	seen := map[string]string{} // module id -> language that first claimed it
	for _, g := range graphs {
		if g == nil {
			continue
		}
		if g.IRVersion != "" && g.IRVersion != Version {
			return nil, fmt.Errorf("cannot merge IR of version %q into version %q", g.IRVersion, Version)
		}
		for _, m := range g.Modules {
			if prev, ok := seen[m.ID]; ok {
				return nil, fmt.Errorf("module id %q derived by both %q and %q derivers; module ids must be disjoint across languages", m.ID, prev, m.Language)
			}
			seen[m.ID] = m.Language
			out.Modules = append(out.Modules, m)
		}
		out.Edges = append(out.Edges, g.Edges...)
		out.Confidence = append(out.Confidence, g.Confidence...)
		out.Analyzers = append(out.Analyzers, g.Analyzers...)
	}
	out.Canonicalize()
	return out, nil
}

// Marshal returns the canonical JSON bytes (alias for Canonical, named for
// symmetry with Unmarshal).
func (g *Graph) Marshal() ([]byte, error) { return g.Canonical() }

// Unmarshal parses canonical IR JSON, e.g. a golden fixture, back into a Graph.
func Unmarshal(b []byte) (*Graph, error) {
	var g Graph
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("parse IR: %w", err)
	}
	if g.IRVersion != Version {
		return nil, fmt.Errorf("IR version %q is not supported (engine speaks %q)", g.IRVersion, Version)
	}
	return &g, nil
}
