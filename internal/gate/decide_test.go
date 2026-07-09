package gate

import (
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// TestDecideTierMatrix pins the gate decision function: Tier A (or a promoted
// rule) blocks, a cannot-verify fails closed, and — the load-bearing M4 property
// — a Tier C violation can NEVER change the exit code, even if its rule id is
// (mistakenly) present in the promoted set. This is the structural proof that the
// judgment-assisted / LLM tier cannot gate a merge (principle 3, NFR-1).
func TestDecideTierMatrix(t *testing.T) {
	v := func(rule string, tier plane.Tier, kind plane.Kind) plane.Violation {
		return plane.Violation{RuleID: rule, Tier: tier, Kind: kind}
	}
	cases := []struct {
		name       string
		violations []plane.Violation
		failClosed []FailClosed
		promoted   map[string]bool
		wantDec    string
		wantExit   int
	}{
		{
			name:       "clean passes",
			violations: []plane.Violation{v("arch.duplication", plane.TierB, plane.KindViolation)},
			wantDec:    "pass", wantExit: ExitPass,
		},
		{
			name:       "tier A blocks",
			violations: []plane.Violation{v("arch.illegal-dependency", plane.TierA, plane.KindViolation)},
			wantDec:    "block", wantExit: ExitBlocked,
		},
		{
			name:       "promoted tier B blocks",
			violations: []plane.Violation{v("arch.duplication", plane.TierB, plane.KindViolation)},
			promoted:   map[string]bool{"arch.duplication": true},
			wantDec:    "block", wantExit: ExitBlocked,
		},
		{
			name:       "unpromoted tier B is advisory (passes)",
			violations: []plane.Violation{v("arch.duplication", plane.TierB, plane.KindViolation)},
			wantDec:    "pass", wantExit: ExitPass,
		},
		{
			name:       "cannot-verify fails closed",
			violations: []plane.Violation{v("arch.illegal-dependency", plane.TierA, plane.KindCannotVerify)},
			wantDec:    "block", wantExit: ExitFailClosed,
		},
		{
			name:       "fail-closed reason blocks",
			failClosed: []FailClosed{{Code: "tool-missing", Message: "no analyzer"}},
			wantDec:    "block", wantExit: ExitFailClosed,
		},
		{
			// The core M4 guarantee: a Tier C violation is excluded from the
			// decision even when someone forced its id into the promoted set.
			name:       "tier C never blocks even if promoted",
			violations: []plane.Violation{v("arch.unclear-name", plane.TierC, plane.KindViolation)},
			promoted:   map[string]bool{"arch.unclear-name": true},
			wantDec:    "pass", wantExit: ExitPass,
		},
		{
			// Defence in depth: a Tier C violation carrying a cannot-verify kind
			// still must not fail the gate — Tier C contributes nothing at all.
			name:       "tier C cannot even fail-closed",
			violations: []plane.Violation{v("arch.data-clump", plane.TierC, plane.KindCannotVerify)},
			wantDec:    "pass", wantExit: ExitPass,
		},
		{
			// A real Tier A block alongside a Tier C advisory blocks on the Tier A
			// alone; the Tier C is reported but irrelevant to the decision.
			name: "tier C rides along with a real block without affecting it",
			violations: []plane.Violation{
				v("arch.cycle", plane.TierA, plane.KindViolation),
				v("arch.feature-envy", plane.TierC, plane.KindViolation),
			},
			wantDec: "block", wantExit: ExitBlocked,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &Outcome{Decision: "pass", Violations: tc.violations, FailClosed: tc.failClosed}
			decide(out, tc.promoted)
			if out.Decision != tc.wantDec {
				t.Errorf("decision = %q, want %q", out.Decision, tc.wantDec)
			}
			if out.ExitCode != tc.wantExit {
				t.Errorf("exit = %d, want %d", out.ExitCode, tc.wantExit)
			}
		})
	}
}
