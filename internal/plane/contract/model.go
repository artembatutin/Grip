package contract

import "sort"

// Model is the contract plane's Derived value — the FOURTH kind of derived model
// the plane contract must carry (after M0's Common Graph IR, M1's mutation scores,
// and M2's I/O snapshots): a VERSIONED/TEMPORAL comparison. For each governed
// module and each contract kind it folds together three points in time — current
// (the code now), declared (the ratified baseline in the working tree), and
// previous (that baseline as of the prior commit) — plus the wrapped checker's
// verdict on current-vs-declared. It is deliberately none of the earlier shapes,
// which is the point of M3: it proves the seam did not overfit.
//
// Everything Reconcile needs is captured here during Derive (all I/O happens
// there — running the per-kind checkers, reading the git-tracked baselines,
// fetching the prior-commit baseline), so Reconcile stays pure and deterministic:
// Modules is sorted by id and each module's kinds are read in canonical order.
//
// Derive gathers only FACTS (is there a ratified baseline? did the checker return a
// verdict? was the baseline re-ratified?); it is governance-agnostic, because it
// receives module refs, not manifests. The governance-aware Reconcile — which holds
// the parsed intents — decides which facts amount to a block, an advisory, or a
// fail-closed cannot-verify.
type Model struct {
	// Modules holds one entry per governed module, sorted by id.
	Modules []*ModuleState

	byID map[string]*ModuleState
}

// ModuleState is one module's contract reality across the kinds Derive observed
// (those with a ratified baseline on disk and/or a checker verdict).
type ModuleState struct {
	ModuleID string
	Language string
	// Kinds maps a kind id (api/events/db) to the facts Derive gathered for it.
	Kinds map[string]*KindState
}

// KindState is the raw, governance-agnostic facts Derive gathered for one
// (module, kind): whether a ratified baseline exists on disk, whether the wrapped
// checker returned a verdict and what it said, the changes it classified, the
// current shape (so ratify can adopt it), and whether the baseline was re-ratified
// versus the prior commit. Reconcile turns these facts into a decision.
type KindState struct {
	Kind string
	// BaselinePresent is true when a ratified baseline artifact exists on disk for
	// this (module, kind). Grip's filesystem view is authoritative: no baseline →
	// cannot-verify, regardless of what a checker claims.
	BaselinePresent bool
	// HasVerdict is true when the checker returned an entry for this (module, kind).
	HasVerdict bool
	// CheckerResolved is the checker's own resolved flag (meaningful only when
	// HasVerdict): false means it could not resolve the current or prior version.
	CheckerResolved bool
	// CheckerReason is the checker's explanation when CheckerResolved is false.
	CheckerReason string
	// Changes are the checker's classified, policy-neutral changes (current vs the
	// declared baseline); empty when the shapes match.
	Changes []Change
	// CurrentShape is the canonical current contract text, carried so `grip ratify
	// contract` can adopt it as the new baseline. Not read by Reconcile.
	CurrentShape string
	// Repinned is true when the declared baseline artifact differs from its
	// prior-commit form — the human re-ratified this contract on purpose. Used only
	// to render an intentional change (principle 5); it never blocks. Its absence
	// (no prior baseline available) is benign.
	Repinned bool
}

// Change is one atomic contract change the wrapped checker reported, tagged with a
// policy-neutral Nature. Element names the affected surface member; Consumer names
// a known downstream that relies on it (for the "this breaks X" report), or is
// empty when unknown.
type Change struct {
	Nature   Nature
	Element  string
	Consumer string
	Detail   string
	File     string
	Line     int
}

// Module returns the state for a module id, or nil.
func (m *Model) Module(id string) *ModuleState {
	if m.byID == nil {
		return nil
	}
	return m.byID[id]
}

// Kind returns the named kind's facts for this module, or nil.
func (st *ModuleState) Kind(kind string) *KindState {
	if st == nil || st.Kinds == nil {
		return nil
	}
	return st.Kinds[kind]
}

// index builds the id lookup, sorts Modules, and sorts each kind's Changes into a
// canonical order. Called once at the end of Derive so Reconcile can rely on
// stable order (no map-iteration-order leaks, NFR-1).
func (m *Model) index() {
	m.byID = make(map[string]*ModuleState, len(m.Modules))
	for _, st := range m.Modules {
		for _, ks := range st.Kinds {
			sortChanges(ks.Changes)
		}
		m.byID[st.ModuleID] = st
	}
	sort.Slice(m.Modules, func(a, b int) bool { return m.Modules[a].ModuleID < m.Modules[b].ModuleID })
}

// sortChanges orders changes deterministically by (element, nature, detail) so a
// report over a set of changes is byte-identical run to run.
func sortChanges(cs []Change) {
	sort.Slice(cs, func(a, b int) bool {
		if cs[a].Element != cs[b].Element {
			return cs[a].Element < cs[b].Element
		}
		if cs[a].Nature != cs[b].Nature {
			return cs[a].Nature < cs[b].Nature
		}
		return cs[a].Detail < cs[b].Detail
	})
}
