package testrigor

import (
	"sort"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// --- builders -------------------------------------------------------------

func newModel(baseline map[string]*BaselineState, states ...*ModuleState) *Model {
	m := &Model{Modules: states, Baseline: baseline}
	m.index()
	return m
}

func tst(id string, behaviors []string, contract, skipped, flaky bool) TestState {
	return TestState{ID: id, Behaviors: behaviors, Contract: contract, Skipped: skipped, Flaky: flaky, File: "test/" + id + ".spec", Line: 3}
}

// contractState builds a module whose boundary-contract summary is set directly
// (as Derive would compute it), for the vacuous/flaky-contract rules.
func contractState(id string, present, flaky bool, mutants, killed int) *ModuleState {
	st := &ModuleState{ModuleID: id, Language: "typescript", Analyzed: true,
		ContractPresent: present, ContractFlaky: flaky, ContractMutants: mutants, ContractKilled: killed,
		ContractTestID: "contract.spec::boundary", ContractFile: "test/contract.spec", ContractLine: 3}
	return st
}

func intent(id string, behaviors []string, threshold int, hasThreshold, boundaryContract bool) Intent {
	return Intent{ModuleID: id, RequiredBehaviors: behaviors, MutationThreshold: threshold,
		HasThreshold: hasThreshold, BoundaryContract: boundaryContract, HasSection: true}
}

func intents(is ...Intent) map[string]plane.Intent {
	m := map[string]plane.Intent{}
	for _, i := range is {
		m[i.ModuleID] = i
	}
	return m
}

func ruleKinds(vs []plane.Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.RuleID+":"+string(v.Kind))
	}
	sort.Strings(out)
	return out
}

func run(intentsMap map[string]plane.Intent, m *Model) []plane.Violation {
	typed := map[string]Intent{}
	for id, raw := range intentsMap {
		typed[id] = raw.(Intent)
	}
	return reconcile(typed, m)
}

func wantRuleKinds(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rule:kind mismatch\n got: %v\nwant: %v", got, want)
	}
}

func wantMsg(t *testing.T, vs []plane.Violation, sub string) {
	t.Helper()
	for _, v := range vs {
		if strings.Contains(v.Message, sub) {
			return
		}
	}
	t.Errorf("no message contains %q; got %+v", sub, vs)
}

// --- table: every rule fires positively and stays silent on a near-miss -----
//
// Each near-miss differs from its positive by exactly one field (killed 0→1,
// threshold </=, score </=, skipped true/false), which pins the reconciler's
// comparison operators: a mutated operator (e.g. `<` → `<=`) breaks a case here.
// That is the practical stand-in for "mutation-test your own reconciler".

func TestReconcileRules(t *testing.T) {
	cases := []struct {
		name    string
		intents map[string]plane.Intent
		model   *Model
		want    []string
		msgSub  string
	}{
		// ---- vacuous-contract (Tier A) ----
		{
			name:    "vacuous-contract fires (all mutants survive)",
			intents: intents(intent("m", nil, 0, false, true)),
			model:   newModel(nil, contractState("m", true, false, 5, 0)),
			want:    []string{"test.vacuous-contract:violation"},
			msgSub:  "kills none of its 5 mutants",
		},
		{
			name:    "vacuous-contract negative (one mutant killed)",
			intents: intents(intent("m", nil, 0, false, true)),
			model:   newModel(nil, contractState("m", true, false, 5, 1)),
			want:    nil,
		},
		{
			name:    "vacuous-contract negative (no mutants to kill)",
			intents: intents(intent("m", nil, 0, false, true)),
			model:   newModel(nil, contractState("m", true, false, 0, 0)),
			want:    nil,
		},

		// ---- flaky contract → cannot-verify (fail-closed) ----
		{
			name:    "flaky contract cannot-verify (never a false pass)",
			intents: intents(intent("m", nil, 0, false, true)),
			model:   newModel(nil, contractState("m", true, true, 5, 0)),
			want:    []string{"test.vacuous-contract:cannotVerify"},
			msgSub:  "is flaky",
		},

		// ---- deleted-required-test (Tier A) ----
		{
			name:    "deleted-required-test fires (baseline test gone)",
			intents: intents(intent("m", []string{"checkout"}, 0, false, false)),
			model: newModel(
				map[string]*BaselineState{"m": {RequiredTests: map[string][]string{"checkout": {"checkout.spec::pays"}}}},
				&ModuleState{ModuleID: "m", Analyzed: true, Tests: nil},
			),
			want:   []string{"test.deleted-required-test:violation", "test.unverified-module:violation"},
			msgSub: "no longer has a test for required behavior \"checkout\"",
		},
		{
			name:    "deleted-required-test negative (test still present)",
			intents: intents(intent("m", []string{"checkout"}, 0, false, false)),
			model: newModel(
				map[string]*BaselineState{"m": {RequiredTests: map[string][]string{"checkout": {"checkout.spec::pays"}}}},
				&ModuleState{ModuleID: "m", Analyzed: true, Tests: []TestState{tst("checkout.spec::pays", []string{"checkout"}, false, false, false)}},
			),
			// unverified still reports: behaviors declared, no boundaryContract.
			want: []string{"test.unverified-module:violation"},
		},
		{
			name:    "deleted-required-test negative (behavior dropped from manifest = intentional)",
			intents: intents(intent("m", nil, 0, false, true)), // checkout no longer required; boundaryContract present + contract killing
			model: newModel(
				map[string]*BaselineState{"m": {RequiredTests: map[string][]string{"checkout": {"checkout.spec::pays"}}}},
				contractState("m", true, false, 3, 3),
			),
			want: nil,
		},

		// ---- skipped-required-test (Tier A) ----
		{
			name:    "skipped-required-test fires",
			intents: intents(intent("m", []string{"checkout"}, 0, false, false)),
			model: newModel(nil,
				&ModuleState{ModuleID: "m", Analyzed: true, Tests: []TestState{tst("checkout.spec::pays", []string{"checkout"}, false, true, false)}}),
			want:   []string{"test.skipped-required-test:violation", "test.unverified-module:violation"},
			msgSub: "verified only by skipped test",
		},
		{
			name:    "skipped-required-test negative (an active test also covers it)",
			intents: intents(intent("m", []string{"checkout"}, 0, false, false)),
			model: newModel(nil, &ModuleState{ModuleID: "m", Analyzed: true, Tests: []TestState{
				tst("checkout.spec::pays", []string{"checkout"}, false, true, false),
				tst("checkout.spec::also", []string{"checkout"}, false, false, false),
			}}),
			want: []string{"test.unverified-module:violation"},
		},

		// ---- flaky required behavior → cannot-verify ----
		{
			name:    "required behavior covered only by flaky test → cannot-verify",
			intents: intents(intent("m", []string{"checkout"}, 0, false, false)),
			model: newModel(nil, &ModuleState{ModuleID: "m", Analyzed: true, Tests: []TestState{
				tst("checkout.spec::pays", []string{"checkout"}, false, false, true),
			}}),
			want:   []string{"test.skipped-required-test:cannotVerify", "test.unverified-module:violation"},
			msgSub: "covered only by flaky test",
		},

		// ---- threshold-tamper (Tier A) ----
		{
			name:    "threshold-tamper fires (lowered vs baseline)",
			intents: intents(intent("m", nil, 50, true, true)),
			model: newModel(map[string]*BaselineState{"m": {Threshold: 80, HasThreshold: true}},
				contractState("m", true, false, 4, 4)),
			want:   []string{"test.threshold-tamper:violation"},
			msgSub: "lowered its mutationThreshold from 80 to 50",
		},
		{
			name:    "threshold-tamper negative (unchanged)",
			intents: intents(intent("m", nil, 80, true, true)),
			model: newModel(map[string]*BaselineState{"m": {Threshold: 80, HasThreshold: true}},
				contractState("m", true, false, 4, 4)),
			want: nil,
		},
		{
			name:    "threshold-tamper negative (raised)",
			intents: intents(intent("m", nil, 90, true, true)),
			model: newModel(map[string]*BaselineState{"m": {Threshold: 80, HasThreshold: true}},
				contractState("m", true, false, 4, 4)),
			want: nil,
		},
		{
			name:    "threshold-tamper negative (no baseline)",
			intents: intents(intent("m", nil, 10, true, true)),
			model:   newModel(nil, contractState("m", true, false, 4, 4)),
			want:    nil,
		},

		// ---- Tier B advisories (non-blocking) ----
		{
			name:    "declining-mutation-score advisory fires",
			intents: intents(intent("m", nil, 0, false, true)),
			model: newModel(map[string]*BaselineState{"m": {MutationScore: 90, HasScore: true}},
				&ModuleState{ModuleID: "m", Analyzed: true, MutationScore: 70, ContractPresent: true, ContractMutants: 3, ContractKilled: 3}),
			want:   []string{"test.declining-mutation-score:violation"},
			msgSub: "fell from 90% to 70%",
		},
		{
			name:    "declining-mutation-score negative (equal)",
			intents: intents(intent("m", nil, 0, false, true)),
			model: newModel(map[string]*BaselineState{"m": {MutationScore: 90, HasScore: true}},
				&ModuleState{ModuleID: "m", Analyzed: true, MutationScore: 90, ContractPresent: true, ContractMutants: 3, ContractKilled: 3}),
			want: nil,
		},
		{
			name:    "rising-mock-ratio advisory fires",
			intents: intents(intent("m", nil, 0, false, true)),
			model: newModel(map[string]*BaselineState{"m": {MockRatio: 10, HasMockRatio: true}},
				&ModuleState{ModuleID: "m", Analyzed: true, MockRatio: 40, ContractPresent: true, ContractMutants: 3, ContractKilled: 3}),
			want:   []string{"test.rising-mock-ratio:violation"},
			msgSub: "rose from 10% to 40%",
		},
		{
			name:    "rising-mock-ratio negative (equal)",
			intents: intents(intent("m", nil, 0, false, true)),
			model: newModel(map[string]*BaselineState{"m": {MockRatio: 10, HasMockRatio: true}},
				&ModuleState{ModuleID: "m", Analyzed: true, MockRatio: 10, ContractPresent: true, ContractMutants: 3, ContractKilled: 3}),
			want: nil,
		},

		// ---- unverified (Tier C, report only) ----
		{
			name:    "unverified fires (behaviors but no boundaryContract)",
			intents: intents(intent("m", []string{"checkout"}, 0, false, false)),
			model: newModel(nil, &ModuleState{ModuleID: "m", Analyzed: true, Tests: []TestState{
				tst("checkout.spec::pays", []string{"checkout"}, false, false, false)}}),
			want:   []string{"test.unverified-module:violation"},
			msgSub: "no verified boundary contract",
		},
		{
			name:    "unverified fires (boundaryContract declared but no contract test)",
			intents: intents(intent("m", nil, 0, false, true)),
			model:   newModel(nil, &ModuleState{ModuleID: "m", Analyzed: true, ContractPresent: false}),
			want:    []string{"test.unverified-module:violation"},
			msgSub:  "no boundary-contract test was found",
		},
		{
			name:    "unverified negative (contract present and killing)",
			intents: intents(intent("m", []string{"checkout"}, 0, false, true)),
			model:   newModel(nil, contractState("m", true, false, 4, 4)),
			want:    nil,
		},

		// ---- opt-out: a module with no testRigor section is silent ----
		{
			name:    "no section = no claims = no violations",
			intents: intents(Intent{ModuleID: "m", HasSection: false}),
			model:   newModel(nil, &ModuleState{ModuleID: "m", Analyzed: true}),
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(tc.intents, tc.model)
			wantRuleKinds(t, ruleKinds(got), tc.want)
			if tc.msgSub != "" {
				wantMsg(t, got, tc.msgSub)
			}
		})
	}
}

// TestReconcileDeterministic asserts reconcile is order-independent: shuffling
// modules and each module's test list yields byte-identical violation output
// (NFR-1, principle 3).
func TestReconcileDeterministic(t *testing.T) {
	m := newModel(
		map[string]*BaselineState{
			"a": {Threshold: 80, HasThreshold: true, RequiredTests: map[string][]string{"pay": {"a.spec::pay"}}},
			"b": {MutationScore: 90, HasScore: true},
		},
		&ModuleState{ModuleID: "a", Analyzed: true, Tests: []TestState{
			tst("a.spec::ship", []string{"ship"}, false, true, false),
			tst("a.spec::refund", []string{"refund"}, false, false, true),
		}},
		&ModuleState{ModuleID: "b", Analyzed: true, MutationScore: 50, ContractPresent: true, ContractMutants: 4, ContractKilled: 0},
	)
	is := intents(
		intent("a", []string{"pay", "ship", "refund"}, 40, true, false),
		intent("b", nil, 0, false, true),
	)

	first := ruleKinds(run(is, m))
	if len(first) == 0 {
		t.Fatal("expected violations to exercise ordering")
	}
	for i := 0; i < 30; i++ {
		reverseModules(m)
		for _, st := range m.Modules {
			reverseTests(st)
		}
		m.index()
		got := ruleKinds(run(is, m))
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("iteration %d: %v != %v", i, got, first)
		}
	}
}

func reverseModules(m *Model) {
	for i, j := 0, len(m.Modules)-1; i < j; i, j = i+1, j-1 {
		m.Modules[i], m.Modules[j] = m.Modules[j], m.Modules[i]
	}
}

func reverseTests(st *ModuleState) {
	for i, j := 0, len(st.Tests)-1; i < j; i, j = i+1, j-1 {
		st.Tests[i], st.Tests[j] = st.Tests[j], st.Tests[i]
	}
}
