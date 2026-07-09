package testrigor

import "sort"

// Model is the test-rigor plane's Derived value. It is deliberately NON-GRAPH —
// mutation scores and a per-module test inventory, not nodes and edges — which is
// the point of M1: it proves the Plane contract (and the engine loop that runs
// it) did not overfit to the architecture plane's Common Graph IR. The engine
// treats it as an opaque plane.Derived; only this plane's Reconcile reads it.
//
// Everything Reconcile needs is captured here during Derive (all I/O happens
// there), so Reconcile stays pure and deterministic: Modules is sorted by id and
// Baseline is lookup-only (never range-iterated for output).
type Model struct {
	// Modules holds one entry per governed, opted-in module, sorted by id.
	Modules []*ModuleState
	// Baseline maps module id -> its prior-commit state, or is nil when no
	// baseline is available (first run). A missing baseline is benign: the
	// comparison rules (tamper, deleted, declining, mock) simply do not fire —
	// you cannot tamper against a baseline that does not exist.
	Baseline map[string]*BaselineState

	byID map[string]*ModuleState
}

// ModuleState is one module's derived test-rigor reality, with flaky tests
// already quarantined from every aggregate (score, contract kills) so a flaky
// signal cannot silently inflate results (the flaky tests remain listed so
// Reconcile can fail closed on them).
type ModuleState struct {
	ModuleID string
	Language string
	// Analyzed is true when the deriver produced results for this module.
	Analyzed bool

	// MutationScore is killed/total as a 0..100 percentage over NON-flaky tests.
	MutationScore int
	// Coverage and MockRatio are 0..100 percentages (ints, not floats, so hashing
	// and reports stay byte-stable — NFR-1).
	Coverage  int
	MockRatio int

	// Tests is the module's test inventory, sorted by id.
	Tests []TestState

	// Contract summary over the module's boundary-contract tests.
	ContractPresent bool   // at least one contract test exists (flaky or not)
	ContractFlaky   bool   // every present contract test is flaky (signal untrustworthy)
	ContractMutants int    // mutants in scope of NON-flaky contract tests
	ContractKilled  int    // mutants killed by NON-flaky contract tests
	ContractTestID  string // representative non-flaky contract test id (else flaky one)
	ContractFile    string
	ContractLine    int
}

// TestState is one test's inventory record.
type TestState struct {
	ID string
	// Behaviors are the required-behavior names this test covers (from the
	// runner's tags/groups/naming).
	Behaviors []string
	// Contract marks a boundary-contract test.
	Contract bool
	// Skipped is the effective skip state: an explicit skip, OR shadowed by a
	// sibling `.only` (jest/mocha) which silently disables it.
	Skipped bool
	// Flaky marks a test whose pass/fail (and thus mutation-kill) signal is
	// non-deterministic and must not be trusted.
	Flaky bool
	File  string
	Line  int
}

// BaselineState is a module's prior-commit test-rigor facts, used only for
// comparison rules. It carries Has* flags so "declared 0" is distinct from
// "absent".
type BaselineState struct {
	Threshold     int
	HasThreshold  bool
	MutationScore int
	HasScore      bool
	MockRatio     int
	HasMockRatio  bool
	// RequiredTests maps a required-behavior name -> the test ids that covered it
	// at baseline. Deleted-required-test fires when such a test id is gone now.
	RequiredTests map[string][]string
}

// Module returns the state for a module id, or nil.
func (m *Model) Module(id string) *ModuleState {
	if m.byID == nil {
		return nil
	}
	return m.byID[id]
}

// index builds the id lookup and sorts Modules + each module's Tests. Called once
// at the end of Derive so Reconcile can rely on canonical order.
func (m *Model) index() {
	m.byID = make(map[string]*ModuleState, len(m.Modules))
	for _, st := range m.Modules {
		sort.Slice(st.Tests, func(a, b int) bool { return st.Tests[a].ID < st.Tests[b].ID })
		m.byID[st.ModuleID] = st
	}
	sort.Slice(m.Modules, func(a, b int) bool { return m.Modules[a].ModuleID < m.Modules[b].ModuleID })
}
