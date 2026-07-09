package contract

import (
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// reconcileOne builds a one-module model from the given kind facts and runs the
// pure reconcile against the module's parsed intent. It calls index() so Reconcile
// sees canonical order, exactly as Derive leaves it.
func reconcileOne(t *testing.T, moduleID, sectionYAML string, kinds ...*KindState) []plane.Violation {
	t.Helper()
	in, err := parseIntent(section(t, sectionYAML), plane.ModuleRef{ID: moduleID})
	if err != nil {
		t.Fatalf("parse intent: %v", err)
	}
	st := &ModuleState{ModuleID: moduleID, Kinds: map[string]*KindState{}}
	for _, ks := range kinds {
		st.Kinds[ks.Kind] = ks
	}
	m := &Model{Modules: []*ModuleState{st}}
	m.index()
	return reconcile(map[string]Intent{moduleID: in}, m)
}

// resolved builds a resolved KindState with the given changes.
func resolved(kind string, changes ...Change) *KindState {
	return &KindState{Kind: kind, BaselinePresent: true, HasVerdict: true, CheckerResolved: true, Changes: changes}
}

func find(vs []plane.Violation, rule string) *plane.Violation {
	for i := range vs {
		if vs[i].RuleID == rule {
			return &vs[i]
		}
	}
	return nil
}

func TestReconcileBreakingAndAdditive(t *testing.T) {
	cases := []struct {
		name     string
		section  string
		kind     string
		change   Change
		wantRule string
		wantKind plane.Kind
		wantTier plane.Tier
		wantNone bool // no violation expected (compatible)
	}{
		// --- breaking-api: positive + policy near-miss ---
		{"api removed blocks (backward)", "api: { compat: backward }", KindAPI,
			Change{Nature: NatureRemoved, Element: "GET /orders#total"}, RuleBreakingAPI, plane.KindViolation, plane.TierA, false},
		{"api required-added blocks (backward)", "api: { compat: backward }", KindAPI,
			Change{Nature: NatureRequiredAdded, Element: "coupon"}, RuleBreakingAPI, plane.KindViolation, plane.TierA, false},
		{"api required-added is safe (forward) — near-miss", "api: { compat: forward }", KindAPI,
			Change{Nature: NatureRequiredAdded, Element: "coupon"}, "", plane.Kind(""), 0, true},

		// --- additive-api: positive + compatible near-miss ---
		{"api optional-added warns", "api: { compat: backward }", KindAPI,
			Change{Nature: NatureOptionalAdded, Element: "nickname"}, RuleAdditiveAPI, plane.KindViolation, plane.TierB, false},
		{"api widened passes clean — near-miss", "api: { compat: backward }", KindAPI,
			Change{Nature: NatureWidened, Element: "status"}, "", plane.Kind(""), 0, true},

		// --- breaking-event: positive + policy near-miss ---
		{"event removed blocks", "events: { compat: backward }", KindEvents,
			Change{Nature: NatureRemoved, Element: "OrderPlaced.total"}, RuleBreakingEvent, plane.KindViolation, plane.TierA, false},
		{"event required-added blocks (full)", "events: { compat: full }", KindEvents,
			Change{Nature: NatureRequiredAdded, Element: "OrderPlaced.currency"}, RuleBreakingEvent, plane.KindViolation, plane.TierA, false},
		{"event required-added safe (forward) — near-miss", "events: { compat: forward }", KindEvents,
			Change{Nature: NatureRequiredAdded, Element: "OrderPlaced.currency"}, "", plane.Kind(""), 0, true},

		// --- breaking-db: positive + additive near-miss ---
		{"db destructive blocks", "db: { compat: backward }", KindDB,
			Change{Nature: NatureDestructive, Element: "orders.total"}, RuleBreakingDB, plane.KindViolation, plane.TierA, false},
		{"db optional-added migration warns — near-miss", "db: { compat: backward }", KindDB,
			Change{Nature: NatureOptionalAdded, Element: "orders.coupon"}, RuleAdditiveDB, plane.KindViolation, plane.TierB, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vs := reconcileOne(t, "m", c.section, resolved(c.kind, c.change))
			if c.wantNone {
				if len(vs) != 0 {
					t.Fatalf("expected no violation, got %+v", vs)
				}
				return
			}
			if len(vs) != 1 {
				t.Fatalf("expected exactly 1 violation, got %d: %+v", len(vs), vs)
			}
			v := vs[0]
			if v.RuleID != c.wantRule || v.Kind != c.wantKind || v.Tier != c.wantTier {
				t.Fatalf("got rule=%s kind=%s tier=%s, want rule=%s kind=%s tier=%s",
					v.RuleID, v.Kind, v.Tier, c.wantRule, c.wantKind, c.wantTier)
			}
			if v.Plane != PlaneID {
				t.Fatalf("plane = %q, want %q", v.Plane, PlaneID)
			}
		})
	}
}

// TestReconcileFailClosed pins every cannot-verify path AND its DISTINCT message,
// so a reordering of the fail-closed guards in classifyKind (which would swap the
// messages) fails here — the ordering is load-bearing, not cosmetic.
func TestReconcileFailClosed(t *testing.T) {
	cases := []struct {
		name       string
		ks         *KindState
		wantSubstr string
	}{
		{"no baseline", &KindState{Kind: KindAPI, BaselinePresent: false}, "no ratified baseline"},
		{"no verdict", &KindState{Kind: KindAPI, BaselinePresent: true, HasVerdict: false}, "returned no verdict"},
		{"unresolved prior", &KindState{Kind: KindAPI, BaselinePresent: true, HasVerdict: true, CheckerResolved: false, CheckerReason: "prior migration unreadable"}, "prior version could not be resolved"},
		{"unknown nature", resolved(KindAPI, Change{Nature: "teleported", Element: "x"}), "unrecognized change kind"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vs := reconcileOne(t, "m", "api: { compat: backward }", c.ks)
			if len(vs) != 1 {
				t.Fatalf("expected 1 cannot-verify violation, got %d: %+v", len(vs), vs)
			}
			v := vs[0]
			if v.Kind != plane.KindCannotVerify {
				t.Fatalf("kind = %s, want cannotVerify", v.Kind)
			}
			if v.RuleID != RuleBreakingAPI {
				t.Fatalf("cannot-verify must carry the breaking rule, got %s", v.RuleID)
			}
			if !strings.Contains(v.Message, c.wantSubstr) {
				t.Fatalf("message %q missing %q", v.Message, c.wantSubstr)
			}
		})
	}
}

func TestReconcileIntentionalRepin(t *testing.T) {
	// Resolved, no changes, but the baseline was re-ratified vs the prior commit →
	// an intentional change, never a block.
	ks := resolved(KindAPI)
	ks.Repinned = true
	vs := reconcileOne(t, "m", "api: { compat: backward }", ks)
	if len(vs) != 1 {
		t.Fatalf("expected 1 intentional violation, got %+v", vs)
	}
	if vs[0].Kind != plane.KindIntentionalChange {
		t.Fatalf("kind = %s, want intentionalChange", vs[0].Kind)
	}
}

func TestReconcileCleanPass(t *testing.T) {
	// Resolved, no changes, not repinned → nothing at all.
	vs := reconcileOne(t, "m", "api: { compat: backward }", resolved(KindAPI))
	if len(vs) != 0 {
		t.Fatalf("expected clean pass, got %+v", vs)
	}
}

func TestReconcileBreakingWinsOverRepin(t *testing.T) {
	// A breaking change on a re-ratified baseline still blocks (never rendered as a
	// benign intentional change). Guards the `len(breaking)==0` condition.
	ks := resolved(KindAPI, Change{Nature: NatureRemoved, Element: "total"})
	ks.Repinned = true
	vs := reconcileOne(t, "m", "api: { compat: backward }", ks)
	if find(vs, RuleBreakingAPI) == nil {
		t.Fatalf("expected breaking violation, got %+v", vs)
	}
	for _, v := range vs {
		if v.Kind == plane.KindIntentionalChange {
			t.Fatalf("a breaking change must not render as intentional: %+v", vs)
		}
	}
}

func TestReconcileOptInSkip(t *testing.T) {
	// A module with no contract section makes no claims, even if the model carries
	// state for it.
	st := &ModuleState{ModuleID: "m", Kinds: map[string]*KindState{KindAPI: resolved(KindAPI, Change{Nature: NatureRemoved, Element: "total"})}}
	m := &Model{Modules: []*ModuleState{st}}
	m.index()
	vs := reconcile(map[string]Intent{"m": {ModuleID: "m", Kinds: map[string]KindIntent{}, HasSection: false}}, m)
	if len(vs) != 0 {
		t.Fatalf("opt-out module must yield no violations, got %+v", vs)
	}
}

func TestReconcileMultipleBreakingNamedSeparately(t *testing.T) {
	// Two removed fields → two violations, each naming its element (the report names
	// WHAT broke, per element).
	vs := reconcileOne(t, "m", "api: { compat: backward }",
		resolved(KindAPI,
			Change{Nature: NatureRemoved, Element: "GET /orders#total"},
			Change{Nature: NatureRemoved, Element: "GET /orders#currency"}))
	if len(vs) != 2 {
		t.Fatalf("expected 2 breaking violations, got %d: %+v", len(vs), vs)
	}
	elems := map[string]bool{}
	for _, v := range vs {
		elems[v.Location.Symbol] = true
	}
	if !elems["GET /orders#total"] || !elems["GET /orders#currency"] {
		t.Fatalf("each broken element must be named, got %+v", elems)
	}
}

func TestReconcileConsumerNamedInMessage(t *testing.T) {
	// "this breaks X": a known consumer is named in the breaking message.
	vs := reconcileOne(t, "checkout", "api: { compat: backward }",
		resolved(KindAPI, Change{Nature: NatureRemoved, Element: "GET /orders#total", Consumer: "billing"}))
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "billing consumer depends on") {
		t.Fatalf("expected consumer named in message, got %+v", vs)
	}
}

// TestReconcileDeterministic shuffles change order and asserts identical output
// (the model sorts changes; reconcile leaks no map order). NFR-1.
func TestReconcileDeterministic(t *testing.T) {
	mk := func(order ...Change) []plane.Violation {
		return reconcileOne(t, "m", "api: { compat: backward }\nevents: { compat: full }", resolved(KindAPI, order...))
	}
	a := mk(Change{Nature: NatureRemoved, Element: "b"}, Change{Nature: NatureRemoved, Element: "a"})
	b := mk(Change{Nature: NatureRemoved, Element: "a"}, Change{Nature: NatureRemoved, Element: "b"})
	if len(a) != len(b) {
		t.Fatalf("length differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Message != b[i].Message {
			t.Fatalf("order-dependent output at %d:\n%q\n%q", i, a[i].Message, b[i].Message)
		}
	}
}
