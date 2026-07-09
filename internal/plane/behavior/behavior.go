// Package behavior is the M2 Behavior plane: the THIRD implementation of the
// plane contract (plan/02, plan/05). It closes the architecture plane's semantic
// blind spot — a change can keep the shape legal and the tests green while quietly
// altering what the system DOES at its boundaries. M2 surfaces that as a diff to
// ratify, not a silent shift.
//
// Its whole reason for existing is to prove the seam holds for a plane whose
// derived model is neither a graph (M0) nor a set of scores (M1) but recorded I/O
// SNAPSHOTS plus a baseline, exercised through a ratify-on-delta workflow. It
// plugs in with ZERO engine changes: it satisfies plane.Plane, is registered
// alongside architecture and test-rigor in internal/cli (the single wiring
// point), and adds a `grip ratify behavior <module>` subcommand there. Nothing in
// internal/gate, internal/reconcile, internal/config, or internal/ir changed.
//
// The defining ergonomic: the human declares nothing about the behavior itself.
// They mark which boundaries to pin (optional `behavior:` section) and ratify
// deltas. The pinned baseline is git-tracked snapshot files (approval-test style),
// never engine state — so there is zero drift by construction. Enforcement is
// deterministic and fail-closed; there is no LLM anywhere in the gate.
package behavior

import (
	"context"

	"github.com/artembatutin/grip/internal/plane"
)

// PlaneID is this plane's stable identity.
const PlaneID = "behavior"

// manifestSection is the top-level grip.yaml key this plane owns.
const manifestSection = "behavior"

// Plane implements plane.Plane for the behavior axis. It holds no state: capture
// happens through DeriveServices.Tools, pins are read from the working tree during
// Derive, and Reconcile is a pure comparison.
type Plane struct{}

// New builds the behavior plane.
func New() *Plane { return &Plane{} }

// ID identifies the plane.
func (p *Plane) ID() string { return PlaneID }

// ManifestSection is the grip.yaml key the plane owns.
func (p *Plane) ManifestSection() string { return manifestSection }

// ParseIntent parses and validates one module's behavior section (strict).
func (p *Plane) ParseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (plane.Intent, error) {
	return parseIntent(raw, mod)
}

// Derive produces the snapshot+baseline model. All I/O — the capture helpers, the
// git-tracked pins, the baseline — lives here behind DeriveServices; Reconcile is
// pure.
func (p *Plane) Derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (plane.Derived, error) {
	return p.derive(ctx, mods, svc)
}

// Reconcile compares declared intent against the derived model. Pure and
// deterministic (no I/O, no clock, no map-order leaks).
func (p *Plane) Reconcile(intents map[string]plane.Intent, derived plane.Derived) []plane.Violation {
	m, ok := derived.(*Model)
	if !ok || m == nil {
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

// Rules statically describes every rule, its tier, and its default, for config
// validation (tier promotion must name a real, promotable rule) and docs.
func (p *Plane) Rules() []plane.RuleSpec {
	return []plane.RuleSpec{
		// Tier A — hard block: a pinned boundary changed without ratification, or
		// (fail-closed) its evidence could not be trusted.
		{ID: RuleUnratifiedChange, Tier: plane.TierA, Summary: "a pinned boundary's observable output changed without an accompanying ratification (GR-BEH-1)"},
		// Tier B — advisory-deterministic, promotable (PRD §9): an observed boundary
		// not yet pinned.
		{ID: RuleUnpinnedBoundary, Tier: plane.TierB, Promotable: true, Summary: "an observed boundary not yet pinned to a snapshot (GR-BEH-2)"},
	}
}

// Ensure the plane satisfies the contract at compile time.
var _ plane.Plane = (*Plane)(nil)
