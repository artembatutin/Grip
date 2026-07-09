package contract

import (
	"sort"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// reconcile is the pure heart of the contract plane: (declared intents, derived
// facts) → located, one-sentence violations. No I/O, no clock, no map-iteration
// leaks — every loop is over a canonical key order, so output is byte-identical
// under shuffled inputs (NFR-1, principle 3). For each governed kind it turns the
// gathered facts into a decision under the module's compat policy: a break blocks
// (Tier A), an additive change advises (Tier B), and a kind whose compatibility
// cannot be established fails closed (cannot-verify) rather than passing. It
// mirrors the architecture, test-rigor, and behavior reconcilers in discipline
// while reading an entirely different — versioned/temporal — model.
func reconcile(intents map[string]Intent, m *Model) []plane.Violation {
	if m == nil {
		return nil
	}
	var vs []plane.Violation

	ids := make([]string, 0, len(intents))
	for id := range intents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		intent := intents[id]
		if !intent.HasSection {
			continue // opt-in: a module with no contract section makes no claims.
		}
		st := m.Module(id)
		for _, kind := range intent.GovernedKinds() {
			ki := intent.Kinds[kind]
			var ks *KindState
			if st != nil {
				ks = st.Kind(kind)
			}
			vs = append(vs, classifyKind(id, ki, ks)...)
		}
	}
	return vs
}

// classifyKind decides the outcome for one governed (module, kind) from the raw
// facts Derive gathered. Ordering is fail-closed first: any condition under which
// we cannot trust the verdict blocks (cannot-verify, exit 2) before any pass or
// advisory path is reached — a governed wire boundary is never assumed compatible.
func classifyKind(mod string, ki KindIntent, ks *KindState) []plane.Violation {
	kind := ki.Kind

	// --- Fail-closed: the module declares it governs this kind, but nothing has
	// been ratified to compare against. Push the human to adopt a baseline.
	if ks == nil || !ks.BaselinePresent {
		return one(cannotVerify(mod, kind, msgNoBaseline(mod, kind, ir.LevelReduced)))
	}
	// A baseline exists but the checker was silent on it — a missing/partial tool
	// output. Never read silence as "unchanged".
	if !ks.HasVerdict {
		return one(cannotVerify(mod, kind, msgNoVerdict(mod, kind, ir.LevelReduced)))
	}
	// The checker ran but could not resolve the current or a prior version a rule
	// needed. This is the "unresolvable prior/declared version touching a rule"
	// case (fail-closed).
	if !ks.CheckerResolved {
		return one(cannotVerify(mod, kind, msgUnresolvedPrior(mod, kind, ks.CheckerReason, ir.LevelReduced)))
	}

	// The checker resolved a comparable contract. Apply the kind's compat policy to
	// each change. An unrecognized nature is itself fail-closed (an unjudged change
	// must never pass), and it taints the whole kind's verdict — surface the
	// cannot-verify and drop any other findings for this kind.
	var breaking, additive []Change
	for _, c := range ks.Changes {
		verdict, ok := classify(c.Nature, ki.Compat)
		if !ok {
			return one(cannotVerify(mod, kind, msgUnknownNature(mod, kind, string(c.Nature), ir.LevelReduced)))
		}
		switch verdict {
		case VerdictBreaking:
			breaking = append(breaking, c)
		case VerdictAdditive:
			additive = append(additive, c)
		}
	}

	var vs []plane.Violation
	for _, c := range breaking {
		vs = append(vs, plane.Violation{
			RuleID: breakingRule(kind), Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
			Location:   loc(mod, kind, c),
			Confidence: ir.LevelFull,
			Message:    msgBreaking(mod, kind, c),
		})
	}
	for _, c := range additive {
		vs = append(vs, plane.Violation{
			RuleID: additiveRule(kind), Plane: PlaneID, Tier: plane.TierB, Kind: plane.KindViolation,
			Location:   loc(mod, kind, c),
			Confidence: ir.LevelFull,
			Message:    msgAdditive(mod, kind, c),
		})
	}

	// A clean, policy-safe change to a re-ratified baseline is the human accepting a
	// new contract on purpose (principle 5): render it as intentional, never a
	// mystery — but it never blocks either way.
	if len(breaking) == 0 && ks.Repinned {
		vs = append(vs, plane.Violation{
			RuleID: breakingRule(kind), Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindIntentionalChange,
			Location:   plane.Location{Module: mod, Symbol: kind},
			Confidence: ir.LevelFull,
			Message:    msgRepinned(mod, kind),
		})
	}
	return vs
}

// cannotVerify builds a fail-closed violation for a governed kind. It carries the
// breaking rule of the kind (the same governed boundary whose compatibility could
// not be established) and KindCannotVerify, which the gate maps to exit 2.
func cannotVerify(mod, kind, msg string) plane.Violation {
	return plane.Violation{
		RuleID: breakingRule(kind), Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
		Location:   plane.Location{Module: mod, Symbol: kind},
		Confidence: ir.LevelReduced,
		Message:    msg,
	}
}

func one(v plane.Violation) []plane.Violation { return []plane.Violation{v} }

func loc(mod, kind string, c Change) plane.Location {
	symbol := c.Element
	if symbol == "" {
		symbol = kind
	}
	return plane.Location{Module: mod, File: c.File, Line: c.Line, Symbol: symbol}
}
