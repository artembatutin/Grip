// Package architecture is the M0 reference implementation of the plane contract
// (plan/02 §5): the first — and until M1, only — Plane. It proves the seam is
// right by governing PHP and TS structure with zero engine coupling. Its Derive
// produces the Common Graph IR; its Reconcile is the pure FR-3…FR-8 rule set.
package architecture

import (
	"context"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// PlaneID is this plane's stable identity.
const PlaneID = "architecture"

// manifestSection is the top-level grip.yaml key this plane owns.
const manifestSection = "architecture"

// Deriver produces the Common Graph IR for a set of governed modules. The
// architecture plane depends only on this narrow interface, not on the concrete
// orchestrator, so the language-derivation machinery stays out of the plane.
type Deriver interface {
	Derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (*ir.Graph, error)
}

// Plane implements plane.Plane for the architecture axis.
type Plane struct {
	deriver Deriver
}

// New builds the architecture plane over a graph deriver (the derive
// orchestrator in production; a stub in tests).
func New(d Deriver) *Plane { return &Plane{deriver: d} }

// ID identifies the plane.
func (p *Plane) ID() string { return PlaneID }

// ManifestSection is the grip.yaml key the plane owns.
func (p *Plane) ManifestSection() string { return manifestSection }

// ParseIntent parses and validates one module's architecture section.
func (p *Plane) ParseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (plane.Intent, error) {
	return parseIntent(raw, mod)
}

// Derive runs the language derivers and bundles the IR with the repo layer order
// into the plane's Model. All I/O lives here; Reconcile is pure.
func (p *Plane) Derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (plane.Derived, error) {
	g, err := p.deriver.Derive(ctx, mods, svc)
	if err != nil {
		return nil, err
	}
	return &Model{
		Graph:      g,
		LayerOrder: append([]string(nil), svc.Layers...),
		Ungoverned: append([]string(nil), svc.Ungoverned...),
	}, nil
}

// Reconcile compares declared intent against the derived model. Pure and
// deterministic (no I/O, no clock, no map-order leaks).
func (p *Plane) Reconcile(intents map[string]plane.Intent, derived plane.Derived) []plane.Violation {
	m, ok := derived.(*Model)
	if !ok || m == nil || m.Graph == nil {
		return nil // wrong Derived type is a wiring bug, not a gate decision.
	}
	typed := make(map[string]Intent, len(intents))
	for id, raw := range intents {
		if in, ok := raw.(Intent); ok {
			typed[id] = in
		}
	}
	return reconcile(typed, m)
}

// Rules statically describes every rule for config validation and docs.
func (p *Plane) Rules() []plane.RuleSpec {
	return []plane.RuleSpec{
		{ID: RuleIllegalDependency, Tier: plane.TierA, Summary: "an outbound dependency not in the module's dependencies.allow (FR-3)"},
		{ID: RuleFacadeWidening, Tier: plane.TierA, Summary: "a symbol used from outside a module but absent from its facade (FR-4)"},
		{ID: RuleCycle, Tier: plane.TierA, Summary: "a dependency cycle among modules (FR-5)"},
		{ID: RuleDirectionViolation, Tier: plane.TierA, Summary: "a dependency pointing outward against the declared layer order (FR-5)"},
		{ID: RuleInternalReach, Tier: plane.TierA, Summary: "a reach into another module's non-facade internals (FR-8)"},
		{ID: RuleStaleDeclaration, Tier: plane.TierA, Summary: "a facade or allow entry with no backing derived reality (FR-6)"},
		// Tier B advisories: declared so .grip.yaml promotions validate against
		// real ids, implemented in M4.
		{ID: RuleDuplication, Tier: plane.TierB, Promotable: true, Summary: "duplicated structure across modules (advisory, M4)"},
		{ID: RuleCoChange, Tier: plane.TierB, Promotable: true, Summary: "modules that always change together (advisory, M4)"},
		{ID: RuleMessageChains, Tier: plane.TierB, Promotable: true, Summary: "long message chains across boundaries (advisory, M4)"},
		{ID: RuleSpeculativeGenerality, Tier: plane.TierB, Promotable: true, Summary: "unused speculative abstraction (advisory, M4)"},
	}
}

// DeclaredSurface returns a module's declared facade and allowed dependencies,
// for shape diffing. A malformed section yields empty lists here — the gate is
// the place that reports a malformed manifest, not the diff.
func DeclaredSurface(raw plane.ManifestSection, modID string) (facade, allow []string) {
	in, err := parseIntent(raw, plane.ModuleRef{ID: modID})
	if err != nil {
		return nil, nil
	}
	return in.Facade, in.Allow
}

// Ensure the plane satisfies the contract at compile time.
var _ plane.Plane = (*Plane)(nil)
