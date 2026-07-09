package behavior

import (
	"sort"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// --- builders -------------------------------------------------------------

func newModel(states ...*ModuleState) *Model {
	m := &Model{Modules: states}
	m.index()
	return m
}

func mod(id string, captured bool, bs ...*BoundaryState) *ModuleState {
	return &ModuleState{ModuleID: id, Language: "typescript", Captured: captured, Boundaries: bs}
}

func bnd(name string) *BoundaryState {
	return &BoundaryState{Name: name, File: "src/" + name + ".ts", Line: 2}
}

// observed marks a boundary as captured with a derived digest (its normalized
// snapshot). reducedB marks it nondeterministic instead. pinnedB attaches a
// git-tracked snapshot digest; baseB the baseline digest.
func observed(b *BoundaryState, d string) *BoundaryState {
	b.Observed, b.DerivedDigest, b.DerivedSnapshot = true, d, "snap-"+d
	return b
}
func reducedB(b *BoundaryState) *BoundaryState { b.Observed, b.Reduced = true, true; return b }
func pinnedB(b *BoundaryState, d string) *BoundaryState {
	b.Pinned, b.PinnedDigest = true, d
	return b
}
func baseB(b *BoundaryState, d string) *BoundaryState {
	b.BasePresent, b.BaseDigest = true, d
	return b
}

func intent(id string, pin ...string) Intent {
	return Intent{ModuleID: id, Pin: pin, HasSection: true}
}
func intents(is ...Intent) map[string]plane.Intent {
	m := map[string]plane.Intent{}
	for _, i := range is {
		m[i.ModuleID] = i
	}
	return m
}

func run(intentsMap map[string]plane.Intent, m *Model) []plane.Violation {
	typed := map[string]Intent{}
	for id, raw := range intentsMap {
		typed[id] = raw.(Intent)
	}
	return reconcile(typed, m)
}

func ruleKinds(vs []plane.Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.RuleID+":"+string(v.Kind))
	}
	sort.Strings(out)
	return out
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
// Each near-miss differs from its positive by exactly one field (derived digest
// ==/≠ pinned, base ==/≠ pinned, reduced true/false, captured true/false, pinned
// true/false). That pins the reconciler's comparisons: a mutated operator (e.g.
// `==` → `!=` on the digest compare, `!=` → `==` on the base compare) breaks a
// case here. That is the practical stand-in for "mutation-test your own
// reconciler".

func TestReconcileRules(t *testing.T) {
	cases := []struct {
		name    string
		intents map[string]plane.Intent
		model   *Model
		want    []string
		msgSub  string
	}{
		// ---- clean match (the load-bearing pass path) ----
		{
			name:    "match: reality equals pin → pass",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d0"), "d0"))),
			want:    nil,
		},

		// ---- unratified-change: drift (Tier A) ----
		{
			name:    "unratified-change fires (reality ≠ pin, not re-pinned)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d1"), "d0"))),
			want:    []string{"behavior.unratified-change:violation"},
			msgSub:  "changed its observable output but the pinned snapshot was not re-ratified",
		},
		{
			name:    "unratified-change negative (behavior-preserving: digests equal)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d0"), "d0"))),
			want:    nil,
		},

		// ---- unratified-change: vanished boundary (Tier A) ----
		{
			name:    "vanished pinned boundary fires (captured but no longer observed)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(bnd("placeOrder"), "d0"))),
			want:    []string{"behavior.unratified-change:violation"},
			msgSub:  "no longer observed at its facade",
		},
		{
			name:    "vanished negative (still observed and matching)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d0"), "d0"))),
			want:    nil,
		},

		// ---- cannot-verify: nondeterministic (fail-closed) ----
		{
			name:    "nondeterministic pinned boundary → cannot-verify (never a false pin)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(reducedB(bnd("placeOrder")), "d0"))),
			want:    []string{"behavior.unratified-change:cannotVerify"},
			msgSub:  "nondeterministic",
		},
		{
			name:    "nondeterministic wanted-but-unpinned → cannot-verify",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, reducedB(bnd("placeOrder")))),
			want:    []string{"behavior.unratified-change:cannotVerify"},
			msgSub:  "nondeterministic",
		},
		{
			name:    "nondeterministic negative (stable capture matching the pin)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d0"), "d0"))),
			want:    nil,
		},

		// ---- cannot-verify: uncaptured module (fail-closed) ----
		{
			name:    "pinned boundary in uncaptured module → cannot-verify",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", false, pinnedB(bnd("placeOrder"), "d0"))),
			want:    []string{"behavior.unratified-change:cannotVerify"},
			msgSub:  "was not captured this run",
		},

		// ---- intentional re-pin (rendered, never blocks) ----
		{
			name:    "re-pin renders intentional (reality == pin, pin != baseline)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, baseB(pinnedB(observed(bnd("placeOrder"), "d1"), "d1"), "d0"))),
			want:    []string{"behavior.unratified-change:intentionalChange"},
			msgSub:  "re-pinned on purpose",
		},
		{
			name:    "re-pin negative (baseline equals current pin → plain match, no note)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, baseB(pinnedB(observed(bnd("placeOrder"), "d1"), "d1"), "d1"))),
			want:    nil,
		},

		// ---- Tier B: marked for pinning but no snapshot yet ----
		{
			name:    "unpinned-boundary fires (wanted, no snapshot file)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true)),
			want:    []string{"behavior.unpinned-boundary:violation"},
			msgSub:  "marked for pinning but has no snapshot yet",
		},
		{
			name:    "unpinned-boundary negative (wanted and pinned+matching)",
			intents: intents(intent("m", "placeOrder")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d0"), "d0"))),
			want:    nil,
		},

		// ---- Tier B: newly observed, not opted in ----
		{
			name:    "newly-observed fires (observed, not pinned, not in pin list)",
			intents: intents(intent("m")), // section present, empty pin
			model:   newModel(mod("m", true, observed(bnd("extra"), "d0"))),
			want:    []string{"behavior.unpinned-boundary:violation"},
			msgSub:  "new observable boundary",
		},
		{
			name:    "newly-observed negative (unstable unwanted boundary stays silent)",
			intents: intents(intent("m")),
			model:   newModel(mod("m", true, reducedB(bnd("extra")))),
			want:    nil,
		},

		// ---- file-as-commitment: a snapshot file gates even when the boundary was
		// dropped from the pin list (approval-test convention; un-gating requires
		// deleting the snapshot, not just editing the manifest — fail-closed). ----
		{
			name:    "pinned file gates a drift even when not in the pin list",
			intents: intents(intent("m")), // section present, pin list empty
			model:   newModel(mod("m", true, pinnedB(observed(bnd("legacy"), "d1"), "d0"))),
			want:    []string{"behavior.unratified-change:violation"},
			msgSub:  "changed its observable output",
		},
		{
			name:    "pinned-but-unlisted boundary that still matches stays silent",
			intents: intents(intent("m")),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("legacy"), "d0"), "d0"))),
			want:    nil,
		},

		// ---- opt-out: no behavior section = no claims ----
		{
			name:    "no section = no claims = no violations",
			intents: intents(Intent{ModuleID: "m", HasSection: false}),
			model:   newModel(mod("m", true, pinnedB(observed(bnd("placeOrder"), "d1"), "d0"))),
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
// modules and each module's boundary list yields byte-identical output (NFR-1,
// principle 3). Any map-iteration leak would surface here.
func TestReconcileDeterministic(t *testing.T) {
	m := newModel(
		mod("a", true,
			pinnedB(observed(bnd("ship"), "d1"), "d0"),             // drift
			baseB(pinnedB(observed(bnd("pay"), "n1"), "n1"), "n0"), // intentional re-pin
		),
		mod("b", true,
			pinnedB(reducedB(bnd("quote")), "d0"), // cannot-verify
			observed(bnd("spy"), "d0"),            // newly-observed Tier B
		),
	)
	is := intents(intent("a", "ship", "pay"), intent("b", "quote"))

	first := ruleKinds(run(is, m))
	if len(first) == 0 {
		t.Fatal("expected violations to exercise ordering")
	}
	for i := 0; i < 30; i++ {
		reverseModules(m)
		for _, st := range m.Modules {
			reverseBoundaries(st)
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

func reverseBoundaries(st *ModuleState) {
	for i, j := 0, len(st.Boundaries)-1; i < j; i, j = i+1, j-1 {
		st.Boundaries[i], st.Boundaries[j] = st.Boundaries[j], st.Boundaries[i]
	}
}
