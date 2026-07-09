package acceptance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/plane/architecture"
)

// floodAdvisor is a stand-in for an LLM that returns a Tier C finding for every
// module — the worst case for "could judgment ever affect the gate?".
type floodAdvisor struct{}

func (floodAdvisor) Judge(_ context.Context, g *ir.Graph) []architecture.JudgmentFinding {
	var out []architecture.JudgmentFinding
	for _, m := range g.Modules {
		out = append(out, architecture.JudgmentFinding{
			Rule:     architecture.RuleUnclearName,
			Location: plane.Location{Module: m.ID},
			Message:  "the model thinks this name is unclear", // content is never asserted
		})
	}
	return out
}

// TestTierCInjectedDoesNotChangeExit is the end-to-end proof of the M4 exit
// criterion "the LLM pass is provably unable to affect the gate decision": run the
// REAL gate over the clean base fixture with the architecture plane wired to an
// advisor that floods every module with Tier C findings, and assert the decision
// and exit code are exactly what the un-advised gate produces (pass / 0). We also
// assert Tier C violations are actually present, so the test is not vacuous, and
// that none of them are Tier A/B.
func TestTierCInjectedDoesNotChangeExit(t *testing.T) {
	fx := fixturesDir(t)
	root := t.TempDir()
	copyTree(t, filepath.Join(fx, "base"), root)

	// A registry identical to the shipped one except the architecture plane is
	// wired with the flooding advisor. Base .grip.yaml enables only architecture,
	// so this is a complete, valid registry for the fixture.
	reg := plane.NewRegistry()
	reg.Register(architecture.New(cli.BuildOrchestrator(), architecture.WithAdvisor(floodAdvisor{})))

	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	tools := &derive.RecordedRunner{AnalysisDir: filepath.Join(root, ".grip-analysis")}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{CI: true, Tools: tools, Commit: "test-commit"})
	if err != nil {
		t.Fatal(err)
	}

	if out.Decision != "pass" || out.ExitCode != gate.ExitPass {
		t.Fatalf("Tier C findings changed the gate decision: decision=%q exit=%d\n%s", out.Decision, out.ExitCode, renderHuman(out))
	}

	var tierC, tierAorB int
	for _, v := range out.Violations {
		switch v.Tier {
		case plane.TierC:
			tierC++
		case plane.TierA, plane.TierB:
			if v.RuleID == architecture.RuleUnclearName {
				tierAorB++ // a judgment finding must never appear as Tier A/B
			}
		}
	}
	if tierC == 0 {
		t.Fatal("expected Tier C judgment violations to be present (else the test proves nothing)")
	}
	if tierAorB != 0 {
		t.Fatalf("a judgment rule leaked into Tier A/B (%d occurrences)", tierAorB)
	}
}
