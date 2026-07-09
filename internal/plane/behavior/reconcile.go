package behavior

import (
	"sort"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// reconcile is the pure heart of the behavior plane: (declared intents, derived
// model) → located, one-sentence violations. No I/O, no clock, no map-iteration
// leaks — every loop is over a sorted key set, so output is byte-identical under
// shuffled inputs (NFR-1, principle 3). It compares reality (the captured,
// normalized snapshot) against the pin (the git-tracked snapshot file). A boundary
// the human wants gated whose evidence cannot be trusted becomes a fail-closed
// cannot-verify result rather than a false pass. It mirrors the architecture and
// test-rigor reconcilers in discipline while reading an entirely different model —
// the proof the seam generalizes to a snapshot+baseline plane.
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
			continue // opt-in: a module with no behavior section makes no claims.
		}
		st := m.Module(id)
		captured := st != nil && st.Captured

		pin := make(map[string]bool, len(intent.Pin))
		for _, b := range intent.Pin {
			pin[b] = true
		}

		// The set of boundaries to consider is the union of those the human marked
		// for pinning and those actually present in the model (observed, pinned, or
		// baseline-known). Iterating a sorted union keeps output stable.
		names := map[string]bool{}
		for b := range pin {
			names[b] = true
		}
		if st != nil {
			for _, b := range st.Boundaries {
				names[b.Name] = true
			}
		}
		ordered := make([]string, 0, len(names))
		for n := range names {
			ordered = append(ordered, n)
		}
		sort.Strings(ordered)

		for _, name := range ordered {
			var b *BoundaryState
			if st != nil {
				b = st.Boundary(name)
			}
			vs = append(vs, classify(id, name, pin[name], captured, b)...)
		}
	}
	return vs
}

// classify decides the single outcome for one boundary. The ordering matters and
// is fail-closed first: an untrustworthy signal on a boundary the human intends
// to gate blocks before any pass path can be reached.
func classify(mod, name string, wanted, captured bool, b *BoundaryState) []plane.Violation {
	observed := b != nil && b.Observed
	pinned := b != nil && b.Pinned
	reduced := b != nil && b.Reduced

	// --- Fail-closed (NFR-9): a boundary we mean to gate (already pinned, or
	// marked for pinning) whose capture is nondeterministic. Trusting it risks a
	// false pass, so block (exit 2) — this is the "flaky boundary degrades to
	// reduced, never a false pin" guarantee.
	if reduced && (pinned || wanted) {
		return one(plane.Violation{
			RuleID: RuleUnratifiedChange, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
			Location:   loc(mod, name, b),
			Confidence: ir.LevelReduced,
			Message:    msgNondeterministic(mod, name, ir.LevelReduced),
		})
	}

	if pinned {
		// A pinned boundary is gated. First the fail-closed case: we could not
		// capture the module's behavior this run, so we cannot confirm the pin holds.
		if !captured {
			return one(plane.Violation{
				RuleID: RuleUnratifiedChange, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
				Location:   loc(mod, name, b),
				Confidence: ir.LevelReduced,
				Message:    msgUncaptured(mod, name, ir.LevelReduced),
			})
		}
		// The module was captured but this boundary is gone — a real, observable
		// change (the facade no longer produces it).
		if !observed {
			return one(plane.Violation{
				RuleID: RuleUnratifiedChange, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
				Location:   loc(mod, name, b),
				Confidence: ir.LevelFull,
				Message:    msgUnratifiedVanished(mod, name),
			})
		}
		// Reality matches the pin → pass. If the pin itself was updated versus the
		// baseline, the human re-pinned on purpose: render it as an intentional
		// change (principle 5), never a mystery — but it never blocks either way.
		if b.DerivedDigest == b.PinnedDigest {
			if b.BasePresent && b.BaseDigest != b.PinnedDigest {
				return one(plane.Violation{
					RuleID: RuleUnratifiedChange, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindIntentionalChange,
					Location:   loc(mod, name, b),
					Confidence: ir.LevelFull,
					Message:    msgRepinned(mod, name),
				})
			}
			return nil // clean match
		}
		// Reality differs from the pin and no re-ratification updated it → block.
		return one(plane.Violation{
			RuleID: RuleUnratifiedChange, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
			Location:   loc(mod, name, b),
			Confidence: ir.LevelFull,
			Message:    msgUnratifiedDrift(mod, name, fileOf(b), lineOf(b)),
		})
	}

	// --- Not pinned. Advisory (Tier B): surface the gap without blocking.
	switch {
	case wanted:
		// The human asked to pin it, but no snapshot exists yet.
		return one(plane.Violation{
			RuleID: RuleUnpinnedBoundary, Plane: PlaneID, Tier: plane.TierB, Kind: plane.KindViolation,
			Location:   loc(mod, name, b),
			Confidence: ir.LevelFull,
			Message:    msgUnpinned(mod, name),
		})
	case observed && !reduced:
		// A stable boundary we saw but the human has not opted to gate. (An unstable
		// unwanted boundary is left silent — advising a pin we would refuse is noise.)
		return one(plane.Violation{
			RuleID: RuleUnpinnedBoundary, Plane: PlaneID, Tier: plane.TierB, Kind: plane.KindViolation,
			Location:   loc(mod, name, b),
			Confidence: ir.LevelFull,
			Message:    msgNewlyObserved(mod, name),
		})
	default:
		return nil // baseline-only remnant with no current relevance
	}
}

func one(v plane.Violation) []plane.Violation { return []plane.Violation{v} }

func loc(mod, name string, b *BoundaryState) plane.Location {
	return plane.Location{Module: mod, File: fileOf(b), Line: lineOf(b), Symbol: name}
}

func fileOf(b *BoundaryState) string {
	if b == nil {
		return ""
	}
	return b.File
}

func lineOf(b *BoundaryState) int {
	if b == nil {
		return 0
	}
	return b.Line
}
