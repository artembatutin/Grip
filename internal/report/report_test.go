package report

import (
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/diff"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/plane"
)

func TestHumanRendersIntentionalFacadeEdit(t *testing.T) {
	v := View{
		Outcome: &gate.Outcome{Decision: "pass", ExitCode: 0, PlanesRun: []string{"architecture"}},
		Delta: &diff.Delta{
			FacadeEdited: []diff.DeclChange{{Module: "src/domain", Added: []string{"OrderId"}}},
		},
	}
	out := Human(v)
	if !strings.Contains(out, "the architect edited src/domain's facade on purpose") {
		t.Fatalf("intentional facade edit not rendered as intentional:\n%s", out)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("expected PASS:\n%s", out)
	}
}

func TestHumanLeadsWithBlocks(t *testing.T) {
	v := View{Outcome: &gate.Outcome{
		Decision: "block", ExitCode: 1, PlanesRun: []string{"architecture"},
		Violations: []plane.Violation{{
			RuleID: "arch.illegal-dependency", Tier: plane.TierA, Kind: plane.KindViolation,
			Location: plane.Location{Module: "src/domain", File: "src/domain/index.ts", Line: 3},
			Message:  "module src/domain depends on src/infra ... remove the dependency.",
		}},
	}}
	out := Human(v)
	if !strings.Contains(out, "BLOCKED") || !strings.Contains(out, "arch.illegal-dependency") {
		t.Fatalf("block not rendered:\n%s", out)
	}
}

func TestSARIFValid(t *testing.T) {
	v := View{Outcome: &gate.Outcome{
		Decision: "block", ExitCode: 1,
		Violations: []plane.Violation{{
			RuleID: "arch.cycle", Tier: plane.TierA, Kind: plane.KindViolation,
			Location: plane.Location{Module: "a", File: "a/i.ts", Line: 1}, Message: "cycle",
		}},
	}}
	b, err := SARIF(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"version": "2.1.0"`, `"ruleId": "arch.cycle"`, `"startLine": 1`, `"name": "grip"`} {
		if !strings.Contains(s, want) {
			t.Errorf("SARIF missing %q:\n%s", want, s)
		}
	}
}
