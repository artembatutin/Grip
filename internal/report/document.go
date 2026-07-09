package report

import (
	"github.com/artembatutin/grip/internal/diff"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Document is the single, self-contained report model the machine renderers emit
// and the read-only viewer consumes. It is a pure projection of the gate outcome
// plus the declared surfaces and the shape delta — no new source of truth. JSON()
// serializes it and HTML() renders it, so the viewer literally "consumes the JSON,
// nothing else": HTML(doc) is a pure function of the same bytes JSON() produces.
//
// Graph and Declared are additive (omitempty) so the report stays a superset of
// the pre-M4 schema — existing consumers ignore the new fields.
type Document struct {
	Decision   string             `json:"decision"`
	ExitCode   int                `json:"exitCode"`
	IRHash     string             `json:"irHash"`
	PlanesRun  []string           `json:"planesRun"`
	Governed   []string           `json:"governed"`
	Ungoverned []string           `json:"ungoverned"`
	Analyzers  []ir.Analyzer      `json:"analyzers"`
	FailClosed []gate.FailClosed  `json:"failClosed"`
	Violations []plane.Violation  `json:"violations"`
	Graph      *ir.Graph          `json:"graph,omitempty"`
	Declared   map[string]Surface `json:"declared,omitempty"`
	Delta      *diff.Delta        `json:"delta,omitempty"`
}

// Surface is a module's declared boundary: the facade (public surface) and the
// allowed outbound dependencies. It is the "declared" side of the viewer's
// allowed-vs-actual overlay. Plane-agnostic plain data — the engine builds it from
// whichever plane owns the manifest surface and hands it over as bytes.
type Surface struct {
	Facade []string `json:"facade,omitempty"`
	Allow  []string `json:"allow,omitempty"`
}

// BuildDocument projects a View into a Document. The graph is surfaced from the
// gate outcome (the same IR the diff and version read); the declared surfaces come
// from the View, which the CLI populates from the module manifests.
func BuildDocument(v View) Document {
	o := v.Outcome
	return Document{
		Decision:   o.Decision,
		ExitCode:   o.ExitCode,
		IRHash:     o.IRHash,
		PlanesRun:  o.PlanesRun,
		Governed:   o.Governed,
		Ungoverned: o.Ungoverned,
		Analyzers:  o.Analyzers,
		FailClosed: o.FailClosed,
		Violations: o.Violations,
		Graph:      o.Graph,
		Declared:   v.Declared,
		Delta:      v.Delta,
	}
}
