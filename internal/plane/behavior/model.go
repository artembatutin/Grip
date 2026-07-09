package behavior

import "sort"

// Model is the behavior plane's Derived value — the THIRD kind of derived model
// the plane contract must carry (after M0's Common Graph IR and M1's mutation
// scores): recorded I/O snapshots plus a baseline. It is deliberately neither a
// graph nor a set of scores, which is the point of M2: it proves the seam did not
// overfit to the earlier planes. The engine treats it as an opaque plane.Derived;
// only this plane's Reconcile reads it.
//
// Everything Reconcile needs is captured here during Derive (all I/O happens
// there — capturing behavior, reading the git-tracked pins, fetching the
// baseline), so Reconcile stays pure and deterministic: Modules is sorted by id
// and each module's Boundaries by name.
type Model struct {
	// Modules holds one entry per governed module, sorted by id.
	Modules []*ModuleState

	byID map[string]*ModuleState
}

// ModuleState is one module's behavior reality: which boundaries were observed
// this run, which are pinned to a git-tracked snapshot, and the baseline digests
// (for rendering a re-pin as intentional).
type ModuleState struct {
	ModuleID string
	Language string
	// Captured is true when the capture helper produced results for this module
	// (its boundary tests ran). A pinned boundary in an UNcaptured module cannot be
	// checked against reality — Reconcile fails closed (cannot-verify) rather than
	// guessing it vanished.
	Captured bool
	// Boundaries is the union of observed, pinned, and baseline-known boundaries,
	// sorted by name.
	Boundaries []*BoundaryState

	byName map[string]*BoundaryState
}

// BoundaryState is one boundary's derived-vs-pinned reality. The three sources —
// Observed (reality now), Pinned (the approved git-tracked snapshot), Base (the
// pin as of the git baseline) — are all folded in during Derive so Reconcile is a
// pure comparison.
type BoundaryState struct {
	Name string
	File string
	Line int

	// Observed reality, captured from real runs and normalized.
	Observed        bool
	Reduced         bool   // nondeterministic / unnormalizable → cannot pin reliably (NFR-9)
	DerivedSnapshot string // canonical normalized text (empty when reduced or unobserved)
	DerivedDigest   string // digest of DerivedSnapshot

	// Pinned baseline: the git-tracked snapshot file (the approved behavior). Its
	// presence is the opt-in — a boundary backed by a snapshot file is gated.
	Pinned       bool
	PinnedDigest string // digest of the pinned file's bytes

	// Base: the pin as of the git baseline (HEAD / base branch), used only to
	// render a re-pin as an intentional change (principle 5). Absent → no baseline
	// available, which is benign (the gate decision never needs it).
	BasePresent bool
	BaseDigest  string
}

// Module returns the state for a module id, or nil.
func (m *Model) Module(id string) *ModuleState {
	if m.byID == nil {
		return nil
	}
	return m.byID[id]
}

// Boundary returns the named boundary's state, or nil.
func (st *ModuleState) Boundary(name string) *BoundaryState {
	if st == nil || st.byName == nil {
		return nil
	}
	return st.byName[name]
}

// index builds the id/name lookups and sorts Modules and each module's
// Boundaries. Called once at the end of Derive so Reconcile can rely on canonical
// order (no map-iteration-order leaks, NFR-1).
func (m *Model) index() {
	m.byID = make(map[string]*ModuleState, len(m.Modules))
	for _, st := range m.Modules {
		sort.Slice(st.Boundaries, func(a, b int) bool { return st.Boundaries[a].Name < st.Boundaries[b].Name })
		st.byName = make(map[string]*BoundaryState, len(st.Boundaries))
		for _, b := range st.Boundaries {
			st.byName[b.Name] = b
		}
		m.byID[st.ModuleID] = st
	}
	sort.Slice(m.Modules, func(a, b int) bool { return m.Modules[a].ModuleID < m.Modules[b].ModuleID })
}
