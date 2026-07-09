// Package contract is the M3 Contract plane: the FOURTH implementation of the
// plane contract (plan/02, plan/06). It governs the boundaries at the wire —
// service APIs, event/message schemas, and database schema — so an agent cannot
// ship a backward-incompatible change to a consumer-facing surface without a red
// build and a "this breaks X" report.
//
// Its reason for existing, beyond the control it delivers, is to prove the seam
// holds for a plane whose derived model is a VERSIONED/TEMPORAL comparison —
// current (code now) vs previous (prior committed shape, from internal/vcs) vs
// declared (the ratified baseline) — with HETEROGENEOUS sub-derivers per contract
// kind. That is neither M0's graph, M1's scores, nor M2's snapshots: it proves the
// contract did not overfit to any one derived-model shape. Each kind wraps an
// existing breaking-change checker (an OpenAPI/schema/migration differ, NFR-8);
// Grip owns only the pure, policy-aware reconcile. There is no bespoke schema
// differ and no LLM anywhere in the gate.
//
// It plugs in with ZERO engine changes: it satisfies plane.Plane, is registered
// alongside architecture, test-rigor, and behavior in internal/cli (the single
// wiring point), and adds a `grip ratify contract <module>` subcommand there.
// Nothing in internal/gate, internal/reconcile, internal/config, or internal/ir
// changed.
package contract

import (
	"context"

	"github.com/artembatutin/grip/internal/plane"
)

// PlaneID is this plane's stable identity.
const PlaneID = "contract"

// manifestSection is the top-level grip.yaml key this plane owns.
const manifestSection = "contract"

// Plane implements plane.Plane for the contract axis. It holds no state: the
// per-kind checkers run through DeriveServices.Tools, the ratified baseline is
// read from the working tree during Derive, and Reconcile is a pure comparison.
type Plane struct{}

// New builds the contract plane.
func New() *Plane { return &Plane{} }

// ID identifies the plane.
func (p *Plane) ID() string { return PlaneID }

// ManifestSection is the grip.yaml key the plane owns.
func (p *Plane) ManifestSection() string { return manifestSection }

// ParseIntent parses and validates one module's contract section (strict).
func (p *Plane) ParseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (plane.Intent, error) {
	return parseIntent(raw, mod)
}

// Derive produces the versioned/temporal model. All I/O — the per-kind checkers,
// the git-tracked baselines, the prior-commit baseline — lives here behind
// DeriveServices; Reconcile is pure.
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
// validation (tier promotion must name a real, promotable rule) and docs. There
// is one breaking (Tier A) and one additive (Tier B, promotable) rule per contract
// kind; the cannot-verify fail-closed results are carried on the breaking rule of
// the affected kind, since they are the same governed boundary whose compatibility
// could not be established.
func (p *Plane) Rules() []plane.RuleSpec {
	var specs []plane.RuleSpec
	for _, k := range kindsInOrder {
		specs = append(specs,
			plane.RuleSpec{ID: breakingRule(k), Tier: plane.TierA, Summary: breakingSummary(k)},
			plane.RuleSpec{ID: additiveRule(k), Tier: plane.TierB, Promotable: true, Summary: additiveSummary(k)},
		)
	}
	return specs
}

// Ensure the plane satisfies the contract at compile time.
var _ plane.Plane = (*Plane)(nil)
