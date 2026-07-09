package acceptance

import (
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// TestAcceptanceMatrix is the M0 gate: every scripted diff must produce the exact
// decision, exit code, and report. Bad diffs block with the correct rule and a
// located one-sentence remedy; good diffs and intentional edits pass.
func TestAcceptanceMatrix(t *testing.T) {
	scenarios := []scenario{
		// --- clean base: passes (the negative near-miss for most rules) ---
		{
			name:         "clean-base-passes",
			wantDecision: "pass",
			wantExit:     0,
			wantContains: []string{"PASS", "ungoverned modules (no grip.yaml): src/legacy"},
		},

		// --- bad agent diffs: each MUST block with the right rule ---
		{
			name:         "illegal-dependency",
			overlay:      "illegal-dependency",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.illegal-dependency",
			wantContains: []string{"module src/infrastructure depends on src/domain", "dependencies.allow"},
		},
		{
			name:         "facade-widening",
			overlay:      "facade-widening",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.facade-widening",
			wantContains: []string{"exposes symbol OrderId", "not in its declared facade"},
		},
		{
			name:         "cycle",
			overlay:      "cycle",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.cycle",
			wantContains: []string{"form a dependency cycle"},
		},
		{
			name:         "direction-violation",
			overlay:      "direction-violation",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.direction-violation",
			wantContains: []string{"against the declared layer order"},
		},
		{
			name:         "internal-reach",
			overlay:      "internal-reach",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.internal-reach",
			wantContains: []string{"reaches internal symbol OrderInternals", "route through src/domain's facade"},
		},
		{
			name:         "stale-declaration",
			overlay:      "stale-declaration",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.stale-declaration",
			wantContains: []string{"declares facade entry LegacyOrder", "no longer exists as an export"},
		},
		{
			name:         "reduced-confidence-cannot-verify",
			overlay:      "reduced-confidence",
			wantDecision: "block",
			wantExit:     2, // fail-closed
			wantContains: []string{"cannot verify", "analysis confidence is reduced"},
		},
		{
			name:         "tool-missing",
			missingTools: map[string]string{"typescript": "install Node + dependency-cruiser"},
			wantDecision: "block",
			wantExit:     2, // fail-closed
			wantContains: []string{"tool-missing"},
		},
		{
			name:         "missing-manifest",
			overlay:      "missing-manifest",
			wantDecision: "block",
			wantExit:     2, // fail-closed
			wantContains: []string{"ungoverned module with no grip.yaml"},
		},
		{
			name:         "php-illegal-dependency",
			overlay:      "php-illegal-dependency",
			wantDecision: "block",
			wantExit:     1,
			wantRule:     "arch.illegal-dependency",
			wantContains: []string{"module app/Infrastructure depends on app/Domain"},
		},

		// --- good diffs: each MUST pass ---
		{
			name:         "good-internal-refactor",
			overlay:      "good-internal-refactor",
			wantDecision: "pass",
			wantExit:     0,
		},
		{
			name:         "good-add-export-and-declare-facade",
			overlay:      "good-add-facade",
			wantDecision: "pass",
			wantExit:     0,
			wantNotRule:  "arch.facade-widening",
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			out, human := runScenario(t, sc)
			if out.Decision != sc.wantDecision {
				t.Errorf("decision = %q, want %q\n%s", out.Decision, sc.wantDecision, human)
			}
			if out.ExitCode != sc.wantExit {
				t.Errorf("exit = %d, want %d\n%s", out.ExitCode, sc.wantExit, human)
			}
			if sc.wantRule != "" && !hasRule(out.Violations, sc.wantRule) {
				t.Errorf("expected rule %q not fired; violations=%s\n%s", sc.wantRule, ruleList(out.Violations), human)
			}
			if sc.wantNotRule != "" && hasRule(out.Violations, sc.wantNotRule) {
				t.Errorf("rule %q fired but must not; %s", sc.wantNotRule, human)
			}
			for _, want := range sc.wantContains {
				if !strings.Contains(human, want) {
					t.Errorf("report missing %q\n%s", want, human)
				}
			}
		})
	}
}

func hasRule(vs []plane.Violation, id string) bool {
	for _, v := range vs {
		if v.RuleID == id {
			return true
		}
	}
	return false
}

func ruleList(vs []plane.Violation) string {
	var ids []string
	for _, v := range vs {
		ids = append(ids, v.RuleID+"/"+string(v.Kind))
	}
	return strings.Join(ids, ", ")
}
