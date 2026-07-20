// Package derive turns external analyzer output into the Common Graph IR. It is
// the ONLY language-aware layer (plan/01 §1): the IR, reconciler, gate, and
// reporter never branch on language. Per D3, Grip does not parse PHP or TS
// itself — each language deriver invokes ecosystem tools (dependency-cruiser +
// ts-morph for TS; deptrac + a php-parser helper for PHP) via a bundled helper
// that emits a normalized AnalyzerReport, and this package folds that report
// into the IR.
//
// The report→IR normalization is language-agnostic and pure, so it is unit- and
// golden-tested against recorded analyzer reports committed under testdata/ —
// fast, offline, deterministic (plan/08 §1).
package derive

import (
	"fmt"
	"sort"

	"github.com/artembatutin/grip/internal/ir"
)

// AnalyzerReport is the normalized output of a language's bundled helper: the
// facts Grip needs, extracted from the wrapped tools. Its shape mirrors what
// ts-morph (TS) and nikic/php-parser (PHP) naturally produce, so the helper is
// thin and Grip owns the graph computation (cycles, reachability, direction).
type AnalyzerReport struct {
	// Tool is the resolved dependency analyzer (dependency-cruiser / deptrac).
	Tool AnalyzerInfo `json:"tool"`
	// SurfaceTool is the resolved surface/AST helper (ts-morph / php-parser).
	SurfaceTool AnalyzerInfo `json:"surfaceTool"`
	// Imports are every cross-file reference the helper resolved, with the
	// referring location and the referenced symbol.
	Imports []ImportRec `json:"imports"`
	// Exports are the symbols each module's public entrypoint exposes. A symbol
	// reachable only via a deep file path is NOT listed here; reaching it is an
	// internal-reach (see rules).
	Exports []ExportRec `json:"exports"`
	// Reduced records scopes the analyzer could not fully resolve (dynamic
	// import(), variable-variables, reflection). These drive fail-closed
	// "cannot verify" outcomes (NFR-9).
	Reduced []ReducedRec `json:"reduced"`
}

// AnalyzerInfo is a resolved tool name+version, captured for reproducibility.
type AnalyzerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ImportRec is one resolved cross-file reference.
type ImportRec struct {
	FromFile string `json:"fromFile"`
	ToFile   string `json:"toFile"`
	Symbol   string `json:"symbol"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"` // import | call | extends | implements
	// PackageOnly records a dependency that does not name a target symbol, such
	// as a Go blank import. It contributes an edge but not facade reachability.
	PackageOnly bool `json:"packageOnly,omitempty"`
	// External is true when the target resolves outside the repo (a package);
	// such references never produce module edges.
	External bool `json:"external"`
}

// ExportRec is one entrypoint-exported symbol of a module.
type ExportRec struct {
	File string `json:"file"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Line int    `json:"line"`
}

// ReducedRec marks a low-confidence scope.
type ReducedRec struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
	Level  string `json:"level"` // reduced | none (default reduced)
}

// Normalize folds one language's AnalyzerReport into a single-language IR graph.
// It is pure and deterministic: given the same report and topology it produces a
// byte-identical (post-canonicalization) graph. moduleOf maps a repo-relative
// file to its owning governed module id (or "" if ungoverned); filesOf lists a
// module's files so the IR records them even when a module has no imports.
func Normalize(language string, rep *AnalyzerReport, moduleIDs []string, moduleOf func(string) string, filesOf func(string) []string, ungovernedOf ...func(string) string) (*ir.Graph, error) {
	if language == "" {
		return nil, fmt.Errorf("derive: empty language")
	}
	g := &ir.Graph{IRVersion: ir.Version}

	// Seed every governed module with its files so the IR is complete even for a
	// module that neither imports nor is imported.
	order := append([]string(nil), moduleIDs...)
	sort.Strings(order)
	mods := map[string]*ir.Module{}
	for _, id := range order {
		mods[id] = &ir.Module{ID: id, Language: language, Files: append([]string(nil), filesOf(id)...)}
	}

	// Exports: attach entrypoint exports to their owning module.
	for _, ex := range rep.Exports {
		owner := moduleOf(ex.File)
		if owner == "" {
			continue // ungoverned file; not our surface
		}
		m := mods[owner]
		if m == nil {
			continue
		}
		m.Exports = append(m.Exports, ir.Export{Name: ex.Name, Kind: ex.Kind, File: ex.File, Line: ex.Line})
		m.Files = appendUnique(m.Files, ex.File)
	}

	// Imports: build module-level edges and derive reachable-from-outside.
	type edgeKey struct{ from, to, kind string }
	edges := map[edgeKey]*ir.Edge{}
	reachable := map[string]map[string]bool{} // toMod -> set of symbols used externally
	for _, im := range rep.Imports {
		if im.External {
			continue
		}
		fromMod := moduleOf(im.FromFile)
		toMod := moduleOf(im.ToFile)
		if fromMod != "" && toMod == "" {
			if len(ungovernedOf) > 0 && ungovernedOf[0] != nil && ungovernedOf[0](im.ToFile) != "" {
				g.Confidence = append(g.Confidence, ir.Confidence{Scope: im.FromFile, Level: ir.LevelNone, Reason: "dependency targets an ungoverned module: " + im.ToFile})
				continue
			}
			return nil, fmt.Errorf("derive: unresolved internal dependency from %q to %q", im.FromFile, im.ToFile)
		}
		if fromMod == "" || toMod == "" {
			continue
		}
		if m := mods[fromMod]; m != nil {
			m.Files = appendUnique(m.Files, im.FromFile)
		}
		if fromMod == toMod {
			continue // intra-module reference; not a module edge
		}
		kind := im.Kind
		if kind == "" {
			kind = "import"
		}
		k := edgeKey{fromMod, toMod, kind}
		e := edges[k]
		if e == nil {
			e = &ir.Edge{From: fromMod, To: toMod, Kind: kind}
			edges[k] = e
		}
		e.Evidence = append(e.Evidence, ir.Evidence{File: im.FromFile, Line: im.Line, Symbol: im.Symbol})
		if !im.PackageOnly {
			if reachable[toMod] == nil {
				reachable[toMod] = map[string]bool{}
			}
			reachable[toMod][im.Symbol] = true
		}
	}

	for id, m := range mods {
		if syms := reachable[id]; syms != nil {
			for s := range syms {
				m.ReachableFromOutside = append(m.ReachableFromOutside, s)
			}
		}
	}

	// Confidence records.
	for _, r := range rep.Reduced {
		lvl := ir.Level(r.Level)
		if lvl == "" {
			lvl = ir.LevelReduced
		} else if lvl != ir.LevelReduced && lvl != ir.LevelNone {
			return nil, fmt.Errorf("derive: unknown confidence level %q", r.Level)
		}
		g.Confidence = append(g.Confidence, ir.Confidence{Scope: r.File, Level: lvl, Reason: r.Reason})
	}

	// Assemble.
	for _, id := range order {
		g.Modules = append(g.Modules, *mods[id])
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, *e)
	}
	if rep.Tool.Name != "" {
		g.Analyzers = append(g.Analyzers, ir.Analyzer{Name: rep.Tool.Name, Version: rep.Tool.Version, Language: language})
	}
	if rep.SurfaceTool.Name != "" {
		g.Analyzers = append(g.Analyzers, ir.Analyzer{Name: rep.SurfaceTool.Name, Version: rep.SurfaceTool.Version, Language: language})
	}
	g.Canonicalize()
	return g, nil
}

func appendUnique(s []string, v string) []string {
	for _, e := range s {
		if e == v {
			return s
		}
	}
	return append(s, v)
}
