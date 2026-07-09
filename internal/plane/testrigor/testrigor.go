// Package testrigor is the M1 Test-Rigor plane: the SECOND implementation of the
// plane contract (plan/02, plan/04). Its whole reason for existing is to prove
// the seam holds for a plane whose derived model is NOT a graph — mutation scores
// and a per-module test inventory instead of nodes and edges — so the M0 engine
// and its plane loop are shown not to have overfit to the architecture plane.
//
// It plugs in with ZERO engine changes: it satisfies plane.Plane and is
// registered alongside architecture in internal/cli (the single wiring point).
// If fitting it had required touching internal/gate, internal/reconcile,
// internal/config, or internal/ir, the seam would have been wrong. It did not.
//
// "Covered with tests" becomes a claim you can bank: the plane wraps existing
// mutation tools (Stryker for TS/JS, Infection for PHP) to verify tests actually
// fail when code is broken, and detects deleted/skipped required tests and
// coverage-threshold tampering against a baseline. Enforcement is deterministic
// and fail-closed; there is no LLM anywhere in the gate.
package testrigor

import (
	"context"

	"github.com/artembatutin/grip/internal/plane"
)

// PlaneID is this plane's stable identity.
const PlaneID = "test-rigor"

// manifestSection is the top-level grip.yaml key this plane owns.
const manifestSection = "testRigor"

// Plane implements plane.Plane for the test-rigor axis.
type Plane struct {
	// newCache builds the per-run mutation cache from the repo root (known only at
	// Derive time). Nil means the default filesystem cache; tests inject an
	// in-memory one. It is the plane's only injected collaborator — every other
	// I/O capability comes through DeriveServices.
	newCache NewCacheFunc
}

// New builds the test-rigor plane. Pass nil for the production filesystem cache;
// tests pass an in-memory cache factory.
func New(newCache NewCacheFunc) *Plane { return &Plane{newCache: newCache} }

// ID identifies the plane.
func (p *Plane) ID() string { return PlaneID }

// ManifestSection is the grip.yaml key the plane owns.
func (p *Plane) ManifestSection() string { return manifestSection }

// ParseIntent parses and validates one module's testRigor section (strict).
func (p *Plane) ParseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (plane.Intent, error) {
	return parseIntent(raw, mod)
}

// Derive produces the non-graph test-rigor model. All I/O — mutation tools, test
// runners, the cache, the baseline — lives here behind DeriveServices; Reconcile
// is pure.
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
		// Tier A — hard blocks.
		{ID: RuleVacuousContract, Tier: plane.TierA, Summary: "a boundary-contract test whose mutants all survive (vacuous)"},
		{ID: RuleDeletedRequiredTest, Tier: plane.TierA, Summary: "a required test present at baseline but now gone"},
		{ID: RuleSkippedRequiredTest, Tier: plane.TierA, Summary: "a required behavior verified only by a skipped test"},
		{ID: RuleThresholdTamper, Tier: plane.TierA, Summary: "a lowered mutation threshold on a governed module"},
		// Tier B — advisory-deterministic, promotable (PRD §9).
		{ID: RuleDecliningMutation, Tier: plane.TierB, Promotable: true, Summary: "mutation score declining vs baseline (advisory)"},
		{ID: RuleRisingMockRatio, Tier: plane.TierB, Promotable: true, Summary: "mock ratio rising vs baseline (advisory)"},
		// Tier C — report only, never blocks (GR-TST-3).
		{ID: RuleUnverifiedModule, Tier: plane.TierC, Summary: "a module that hides internals but has no verified boundary contract"},
	}
}

// Ensure the plane satisfies the contract at compile time.
var _ plane.Plane = (*Plane)(nil)
