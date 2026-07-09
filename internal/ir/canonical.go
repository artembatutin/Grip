package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// sortStrings sorts a slice of strings in place. Kept as a helper so intent is
// obvious at call sites and so we never accidentally rely on input order.
func sortStrings(s []string) { sort.Strings(s) }

// Canonicalize sorts every slice in the graph into a total, stable order and
// deduplicates where duplicates are meaningless. It is idempotent. This is the
// mechanism behind NFR-1: two derivations of the same commit differ only in the
// order the analyzers happened to emit things, and canonicalization erases that
// difference before hashing or output.
func (g *Graph) Canonicalize() {
	for i := range g.Modules {
		m := &g.Modules[i]
		m.Files = dedupSorted(m.Files)
		m.ReachableFromOutside = dedupSorted(m.ReachableFromOutside)
		sort.Slice(m.Exports, func(a, b int) bool { return exportLess(m.Exports[a], m.Exports[b]) })
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		sort.Slice(e.Evidence, func(a, b int) bool { return evidenceLess(e.Evidence[a], e.Evidence[b]) })
		e.Evidence = dedupEvidence(e.Evidence)
	}
	sort.Slice(g.Modules, func(a, b int) bool { return g.Modules[a].ID < g.Modules[b].ID })
	sort.Slice(g.Edges, func(a, b int) bool { return edgeLess(g.Edges[a], g.Edges[b]) })
	sort.Slice(g.Confidence, func(a, b int) bool { return confidenceLess(g.Confidence[a], g.Confidence[b]) })
	sort.Slice(g.Analyzers, func(a, b int) bool { return analyzerLess(g.Analyzers[a], g.Analyzers[b]) })
}

func exportLess(a, b Export) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Kind < b.Kind
}

func evidenceLess(a, b Evidence) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Symbol < b.Symbol
}

func edgeLess(a, b Edge) bool {
	if a.From != b.From {
		return a.From < b.From
	}
	if a.To != b.To {
		return a.To < b.To
	}
	return a.Kind < b.Kind
}

func confidenceLess(a, b Confidence) bool {
	if a.Scope != b.Scope {
		return a.Scope < b.Scope
	}
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	return a.Reason < b.Reason
}

func analyzerLess(a, b Analyzer) bool {
	if a.Language != b.Language {
		return a.Language < b.Language
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Version < b.Version
}

func dedupSorted(s []string) []string {
	if len(s) == 0 {
		return s
	}
	cp := append([]string(nil), s...)
	sortStrings(cp)
	out := cp[:0]
	var last string
	for i, v := range cp {
		if i == 0 || v != last {
			out = append(out, v)
			last = v
		}
	}
	return out
}

func dedupEvidence(s []Evidence) []Evidence {
	if len(s) <= 1 {
		return s
	}
	out := s[:0]
	var last Evidence
	for i, v := range s {
		if i == 0 || v != last {
			out = append(out, v)
			last = v
		}
	}
	return out
}

// Canonical returns the canonical JSON encoding of the graph: the graph is
// canonicalized (on a clone, so the caller's graph is untouched) and marshaled
// with struct field order fixed and no maps, yielding byte-identical output for
// equal content.
func (g *Graph) Canonical() ([]byte, error) {
	c := g.Clone()
	c.Canonicalize()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal IR: %w", err)
	}
	return append(b, '\n'), nil
}

// Hash returns the hex-encoded SHA-256 of the canonical IR. This is the value
// asserted in the determinism CI matrix (NFR-1). The commit field is included:
// the same structural graph at two different commits legitimately hashes
// differently, and callers who want structure-only equality clear Commit first.
func (g *Graph) Hash() string {
	b, err := g.Canonical()
	if err != nil {
		// Canonical only fails if the graph contains an unmarshalable value,
		// which our concrete types never do; treat as a programmer error.
		panic(fmt.Sprintf("ir: canonical encoding failed: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Clone returns a deep copy of the graph so canonicalization and merging never
// mutate a caller's data (Reconcile must stay pure).
func (g *Graph) Clone() *Graph {
	c := &Graph{
		IRVersion: g.IRVersion,
		Commit:    g.Commit,
	}
	if g.Modules != nil {
		c.Modules = make([]Module, len(g.Modules))
		for i, m := range g.Modules {
			cm := m
			cm.Files = append([]string(nil), m.Files...)
			cm.Exports = append([]Export(nil), m.Exports...)
			cm.ReachableFromOutside = append([]string(nil), m.ReachableFromOutside...)
			c.Modules[i] = cm
		}
	}
	if g.Edges != nil {
		c.Edges = make([]Edge, len(g.Edges))
		for i, e := range g.Edges {
			ce := e
			ce.Evidence = append([]Evidence(nil), e.Evidence...)
			c.Edges[i] = ce
		}
	}
	if g.Confidence != nil {
		c.Confidence = append([]Confidence(nil), g.Confidence...)
	}
	if g.Analyzers != nil {
		c.Analyzers = append([]Analyzer(nil), g.Analyzers...)
	}
	return c
}
