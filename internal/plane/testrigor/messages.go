package testrigor

import (
	"fmt"

	"github.com/artembatutin/grip/internal/ir"
)

// Stable rule ids. Like the architecture plane's, these are part of Grip's public
// contract: they appear in reports, SARIF, and .grip.yaml promotions, so they
// change only deliberately. The "test." prefix mirrors "arch." — the rule id
// namespace is per-plane.
const (
	// Tier A — hard blocks.
	RuleVacuousContract     = "test.vacuous-contract"
	RuleDeletedRequiredTest = "test.deleted-required-test"
	RuleSkippedRequiredTest = "test.skipped-required-test"
	RuleThresholdTamper     = "test.threshold-tamper"

	// Tier B — advisory-deterministic (promotable per PRD §9).
	RuleDecliningMutation = "test.declining-mutation-score"
	RuleRisingMockRatio   = "test.rising-mock-ratio"

	// Tier C — judgment/report only (never blocks, not promotable): the
	// unearned-trust surface (GR-TST-3).
	RuleUnverifiedModule = "test.unverified-module"
)

// Every user-facing string is one plain sentence: rule, what, and remedy (NFR-5).
// They are asserted verbatim in golden tests — a change here is a visible,
// reviewed diff. No timestamps, no absolute paths: reports stay byte-stable.

func msgVacuousContract(mod, testID, file string, line, mutants int) string {
	return fmt.Sprintf("module %s's boundary-contract test %s at %s:%d kills none of its %d mutants — the contract is vacuous; assert real behavior so a broken implementation fails the test.",
		mod, testID, file, line, mutants)
}

func msgDeletedRequiredTest(mod, behavior, baselineTest string) string {
	return fmt.Sprintf("module %s no longer has a test for required behavior %q (covered by %s at baseline) — restore the test or drop %q from the module's requiredBehaviors.",
		mod, behavior, baselineTest, behavior)
}

func msgSkippedRequiredTest(mod, behavior, testID string) string {
	return fmt.Sprintf("module %s's required behavior %q is verified only by skipped test %s — re-enable the test so the behavior is actually exercised.",
		mod, behavior, testID)
}

func msgThresholdTamper(mod string, from, to int) string {
	return fmt.Sprintf("module %s lowered its mutationThreshold from %d to %d — a governed threshold may not be silently weakened; restore it or record the change as intentional.",
		mod, from, to)
}

func msgDecliningMutation(mod string, from, to int) string {
	return fmt.Sprintf("module %s's mutation score fell from %d%% to %d%% — add tests that kill the surviving mutants to restore effectiveness.",
		mod, from, to)
}

func msgRisingMockRatio(mod string, from, to int) string {
	return fmt.Sprintf("module %s's mock ratio rose from %d%% to %d%% — the unit under test is increasingly mocked; assert against real collaborators where you can.",
		mod, from, to)
}

func msgUnverifiedModule(mod string) string {
	return fmt.Sprintf("module %s declares required behaviors but no verified boundary contract — add a boundaryContract test so its hidden internals are trusted through a checked boundary (reported, not blocking).",
		mod)
}

func msgUnverifiedMissingContract(mod string) string {
	return fmt.Sprintf("module %s declares boundaryContract: true but no boundary-contract test was found — add the contract test so the declared boundary is actually verified (reported, not blocking).",
		mod)
}

// Fail-closed (cannot-verify) messages: a flaky signal touching a rule must never
// silently pass — it blocks (exit 2) rather than being trusted (NFR-9).

func msgFlakyContract(mod, testID string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's boundary contract because its contract test %s is flaky (confidence %s) — quarantine and stabilize it so its mutation signal is trustworthy.",
		mod, testID, level)
}

func msgFlakyRequired(mod, behavior, testID string, level ir.Level) string {
	return fmt.Sprintf("cannot verify required behavior %q of module %s because it is covered only by flaky test %s (confidence %s) — stabilize the test so its result can be trusted.",
		behavior, mod, testID, level)
}
