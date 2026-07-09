// Package reconcile is the generic, plane-agnostic engine step: for one plane it
// parses each governed module's intent, derives the plane's actual state, and
// runs the plane's pure Reconcile. It names no plane and branches on none — the
// gate loops it over enabled planes (plan/02 §3). Any parse or derive failure is
// returned as a fail-closed error for the gate to translate into a block.
package reconcile

import (
	"context"
	"fmt"

	"github.com/artembatutin/grip/internal/plane"
)

// Module pairs a governed module reference with its raw manifest section for the
// plane being run.
type Module struct {
	Ref     plane.ModuleRef
	Section plane.ManifestSection
}

// Result is one plane's contribution to the gate.
type Result struct {
	PlaneID    string
	Violations []plane.Violation
	// Derived is the plane's actual-state model, surfaced so the engine can
	// read a Common Graph IR out of it (for diff/report/version) without knowing
	// the plane's concrete type.
	Derived plane.Derived
}

// IntentError marks a failure to parse/validate a module's manifest section. The
// gate maps it to a usage error (exit 3) — the human wrote an invalid manifest —
// distinct from a tool/analysis failure (exit 2).
type IntentError struct {
	Plane  string
	Module string
	Err    error
}

func (e *IntentError) Error() string {
	return fmt.Sprintf("plane %s: module %s: %v", e.Plane, e.Module, e.Err)
}

func (e *IntentError) Unwrap() error { return e.Err }

// RunPlane executes the full plane loop for a single plane: ParseIntent (per
// module, fail-closed on error) → Derive (fail-closed on tool error) →
// Reconcile (pure). It is deterministic given deterministic Derive output.
func RunPlane(ctx context.Context, p plane.Plane, mods []Module, svc plane.DeriveServices) (*Result, error) {
	intents := make(map[string]plane.Intent, len(mods))
	refs := make([]plane.ModuleRef, 0, len(mods))
	for _, m := range mods {
		in, err := p.ParseIntent(m.Section, m.Ref)
		if err != nil {
			return nil, &IntentError{Plane: p.ID(), Module: m.Ref.ID, Err: err}
		}
		intents[m.Ref.ID] = in
		refs = append(refs, m.Ref)
	}
	derived, err := p.Derive(ctx, refs, svc)
	if err != nil {
		return nil, fmt.Errorf("plane %s: derive: %w", p.ID(), err)
	}
	vs := p.Reconcile(intents, derived)
	// Stamp the plane id defensively so a plane that forgets to set it still
	// reports correctly (the engine owns aggregation, not the plane).
	for i := range vs {
		if vs[i].Plane == "" {
			vs[i].Plane = p.ID()
		}
	}
	return &Result{PlaneID: p.ID(), Violations: vs, Derived: derived}, nil
}
