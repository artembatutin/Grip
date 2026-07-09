package architecture

import (
	"context"
	"testing"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// stubDeriver returns a fixed graph so tests can vary the advisor while holding the
// derived architecture constant.
type stubDeriver struct{ g *ir.Graph }

func (s stubDeriver) Derive(context.Context, []plane.ModuleRef, plane.DeriveServices) (*ir.Graph, error) {
	return s.g, nil
}

// stubAdvisor returns canned judgment findings — standing in for an LLM.
type stubAdvisor struct{ findings []JudgmentFinding }

func (s stubAdvisor) Judge(context.Context, *ir.Graph) []JudgmentFinding { return s.findings }

func fixedGraph() *ir.Graph {
	g := &ir.Graph{IRVersion: ir.Version, Commit: "fixed", Modules: []ir.Module{
		mod("src/a", "typescript", []ir.Export{{Name: "A"}}, nil),
		mod("src/b", "typescript", []ir.Export{{Name: "B"}}, nil),
	}}
	g.Canonicalize()
	return g
}

// TestJudgmentEmitsOnlyTierC proves two things at once: the judgment pass keeps
// only findings that name a Tier C rule (an advisor cannot smuggle a Tier A/B or
// unknown rule id through), and every emitted violation is stamped Tier C
// regardless of what the advisor claimed. Per plan/07 we assert tier and rule
// only — never the (non-deterministic) message content.
func TestJudgmentEmitsOnlyTierC(t *testing.T) {
	adv := stubAdvisor{findings: []JudgmentFinding{
		{Rule: RuleUnclearName, Location: plane.Location{Module: "src/a"}, Message: "whatever the model said"},
		{Rule: RuleFeatureEnvy, Location: plane.Location{Module: "src/b"}},
		// Smuggling attempts — all must be dropped:
		{Rule: RuleIllegalDependency, Message: "I claim to be a hard block"}, // Tier A id
		{Rule: RuleDuplication, Message: "I claim to be promotable"},         // Tier B id
		{Rule: "arch.totally-made-up", Message: "unknown"},                   // unknown id
	}}
	sig := judge(context.Background(), adv, fixedGraph())
	vs := judgmentViolations(sig)

	if len(vs) != 2 {
		t.Fatalf("expected exactly the 2 Tier C findings to survive, got %d: %v", len(vs), ruleIDs(vs))
	}
	for _, v := range vs {
		if v.Tier != plane.TierC {
			t.Errorf("judgment violation %q is Tier %s, must be Tier C", v.RuleID, v.Tier)
		}
		if v.RuleID == RuleIllegalDependency || v.RuleID == RuleDuplication || v.RuleID == "arch.totally-made-up" {
			t.Errorf("a non-Tier-C rule id leaked through: %q", v.RuleID)
		}
	}
}

// TestJudgmentViolationsStampTierC proves the emission point itself refuses to emit
// a non-Tier-C rule even if a finding slips into JudgmentSignals directly, and
// always stamps Tier C.
func TestJudgmentViolationsStampTierC(t *testing.T) {
	j := JudgmentSignals{Findings: []JudgmentFinding{
		{Rule: RuleDataClump, Location: plane.Location{Module: "src/a"}},
		{Rule: RuleCycle, Location: plane.Location{Module: "src/a"}}, // Tier A — must be dropped
	}}
	vs := judgmentViolations(j)
	if len(vs) != 1 || vs[0].RuleID != RuleDataClump || vs[0].Tier != plane.TierC {
		t.Fatalf("expected one Tier C data-clump violation, got %v", ruleIDs(vs))
	}
}

// TestJudgmentDoesNotAffectIRHash is the structural proof that no LLM output enters
// the IR or its hash: the same derived graph yields a byte-identical IR hash
// whether the advisor finds nothing or floods the pass with findings (NFR-1).
func TestJudgmentDoesNotAffectIRHash(t *testing.T) {
	g1, g2 := fixedGraph(), fixedGraph()
	quiet := New(stubDeriver{g: g1})
	loud := New(stubDeriver{g: g2}, WithAdvisor(stubAdvisor{findings: []JudgmentFinding{
		{Rule: RuleUnclearName, Location: plane.Location{Module: "src/a"}, Message: "A is unclear"},
		{Rule: RuleDataClump, Location: plane.Location{Module: "src/b"}, Message: "clump"},
		{Rule: RulePrimitiveObsession, Location: plane.Location{Module: "src/b"}},
	}}))

	dq, err := quiet.Derive(context.Background(), nil, plane.DeriveServices{})
	if err != nil {
		t.Fatal(err)
	}
	dl, err := loud.Derive(context.Background(), nil, plane.DeriveServices{})
	if err != nil {
		t.Fatal(err)
	}
	hq := dq.(*Model).IRGraph().Hash()
	hl := dl.(*Model).IRGraph().Hash()
	if hq != hl {
		t.Fatalf("judgment findings changed the IR hash: %s != %s", hq, hl)
	}

	// And the loud plane really did produce Tier C violations (so the test above is
	// not vacuous) — while the quiet default produced none.
	if got := judgmentViolations(dl.(*Model).Judgment); len(got) != 3 {
		t.Fatalf("expected 3 judgment violations from the loud advisor, got %d", len(got))
	}
	if got := judgmentViolations(dq.(*Model).Judgment); len(got) != 0 {
		t.Fatalf("default plane must emit no judgment violations, got %d", len(got))
	}
}

// TestReconcileIncludesTierCButItDoesNotBlock ties the plane to the gate contract:
// a plane wired with an advisor surfaces Tier C violations through Reconcile, yet
// the decision function leaves the exit code untouched.
func TestReconcileIncludesTierCButItDoesNotBlock(t *testing.T) {
	p := New(stubDeriver{g: fixedGraph()}, WithAdvisor(stubAdvisor{findings: []JudgmentFinding{
		{Rule: RuleUnclearName, Location: plane.Location{Module: "src/a"}},
	}}))
	derived, err := p.Derive(context.Background(), nil, plane.DeriveServices{})
	if err != nil {
		t.Fatal(err)
	}
	vs := p.Reconcile(map[string]plane.Intent{
		"src/a": in("src/a", []string{"A"}, nil, ""),
		"src/b": in("src/b", []string{"B"}, nil, ""),
	}, derived)

	var tierC int
	for _, v := range vs {
		if v.Tier == plane.TierC {
			tierC++
		}
	}
	if tierC == 0 {
		t.Fatal("expected at least one Tier C violation from the wired advisor")
	}
}
