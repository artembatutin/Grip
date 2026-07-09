package architecture

import (
	"testing"

	"github.com/artembatutin/grip/internal/ir"
)

// TestGoldenMessages pins every user-facing sentence. Each message is one plain
// sentence naming the rule, the location, and a remedy (NFR-5). Changing one of
// these strings is a deliberate, reviewed diff — that is the whole point of
// treating them as golden.
func TestGoldenMessages(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{
			msgIllegalDependency("src/billing", "src/notifications", "src/billing/invoice.ts", 3),
			"module src/billing depends on src/notifications at src/billing/invoice.ts:3, which is not in its allowed dependencies — add src/notifications to src/billing's dependencies.allow or remove the dependency.",
		},
		{
			msgFacadeWidening("src/billing", "createInvoice", "src/billing/index.ts", 12),
			"module src/billing exposes symbol createInvoice (used from outside) at src/billing/index.ts:12, which is not in its declared facade — add createInvoice to src/billing's facade or stop exposing it.",
		},
		{
			msgCycle([]string{"src/a", "src/b"}),
			"modules src/a → src/b → src/a form a dependency cycle — break it by removing one of the edges so the dependency graph is acyclic.",
		},
		{
			msgDirectionViolation("src/domain", "domain", "src/infra", "infrastructure", "src/domain/x.ts", 4, []string{"domain", "application", "infrastructure"}),
			"module src/domain (layer domain) depends on src/infra (layer infrastructure) at src/domain/x.ts:4 against the declared layer order [domain → application → infrastructure] — dependencies must not point outward across layers.",
		},
		{
			msgInternalReach("src/app", "src/billing", "secretHelper", "src/app/x.ts", 7),
			"module src/app reaches internal symbol secretHelper of module src/billing at src/app/x.ts:7 — route through src/billing's facade instead of its internals.",
		},
		{
			msgStaleFacade("src/billing", "gone"),
			"module src/billing declares facade entry gone which no longer exists as an export — remove gone from src/billing's facade or restore the export.",
		},
		{
			msgStaleAllow("src/billing", "src/phantom"),
			"module src/billing allows dependency src/phantom which is not a governed module or declared layer — fix or remove the entry in src/billing's dependencies.allow.",
		},
		{
			msgMissingManifest("src/app", "src/legacy"),
			"module src/app depends on src/legacy, which is an ungoverned module with no grip.yaml — add a grip.yaml to src/legacy so its boundary can be verified, or remove the dependency.",
		},
		{
			msgCannotVerify("src/infra", "the dependency boundary", "src/infra/dyn.ts", ir.LevelReduced, "dynamic dispatch"),
			"cannot verify the dependency boundary for module src/infra at src/infra/dyn.ts because analysis confidence is reduced (dynamic dispatch) — resolve the dynamic construct or add an explicit declaration so the boundary can be checked.",
		},
	}
	for i, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("case %d message mismatch:\n got: %s\nwant: %s", i, tc.got, tc.want)
		}
	}
}
