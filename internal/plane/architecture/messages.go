package architecture

import (
	"fmt"
	"strings"

	"github.com/artembatutin/grip/internal/ir"
)

// Stable rule ids. These are part of Grip's public contract: they appear in
// reports, SARIF, and .grip.yaml promotions, so they change only deliberately.
const (
	RuleIllegalDependency  = "arch.illegal-dependency"
	RuleFacadeWidening     = "arch.facade-widening"
	RuleCycle              = "arch.cycle"
	RuleDirectionViolation = "arch.direction-violation"
	RuleInternalReach      = "arch.internal-reach"
	RuleStaleDeclaration   = "arch.stale-declaration"

	// Tier B advisories: deterministic, reported, non-blocking by default, but a
	// repo may promote any of them to Tier A via .grip.yaml policy.promote (M0.6).
	// duplication/co-change/message-chains/speculative-generality were declared in
	// M0 so promotion could validate against real ids; middle-man and complexity
	// are added in M4. All are implemented in M4 from wrapped analyzers.
	RuleDuplication           = "arch.duplication"
	RuleCoChange              = "arch.co-change"
	RuleMessageChains         = "arch.message-chains"
	RuleSpeculativeGenerality = "arch.speculative-generality"
	RuleMiddleMan             = "arch.middle-man"
	RuleComplexity            = "arch.complexity"

	// Tier C judgment-assisted rules (M4). These are the ONLY rules that may
	// consult an LLM. They emit ONLY Tier C violations, are marked non-blocking in
	// every report, and can NEVER change the gate decision: the engine excludes
	// Tier C from the decision structurally (gate.decide) and config refuses to
	// promote a Tier C rule. Their derivation is quarantined in Derive so no LLM
	// output ever enters the IR, the IR hash, or any deterministic path (NFR-1).
	RuleUnclearName        = "arch.unclear-name"
	RuleDataClump          = "arch.data-clump"
	RulePrimitiveObsession = "arch.primitive-obsession"
	RuleFeatureEnvy        = "arch.feature-envy"
)

// Every user-facing string is one plain sentence: rule, what, and remedy
// (NFR-5). They are asserted verbatim in golden tests — a change here is a
// visible, reviewed diff.

func msgIllegalDependency(from, to, file string, line int) string {
	return fmt.Sprintf("module %s depends on %s at %s:%d, which is not in its allowed dependencies — add %s to %s's dependencies.allow or remove the dependency.",
		from, to, file, line, to, from)
}

func msgFacadeWidening(mod, symbol, file string, line int) string {
	return fmt.Sprintf("module %s exposes symbol %s (used from outside) at %s:%d, which is not in its declared facade — add %s to %s's facade or stop exposing it.",
		mod, symbol, file, line, symbol, mod)
}

func msgCycle(members []string) string {
	return fmt.Sprintf("modules %s form a dependency cycle — break it by removing one of the edges so the dependency graph is acyclic.",
		strings.Join(members, " → ")+" → "+members[0])
}

func msgDirectionViolation(from, fromLayer, to, toLayer, file string, line int, order []string) string {
	return fmt.Sprintf("module %s (layer %s) depends on %s (layer %s) at %s:%d against the declared layer order [%s] — dependencies must not point outward across layers.",
		from, fromLayer, to, toLayer, file, line, strings.Join(order, " → "))
}

func msgInternalReach(from, to, symbol, file string, line int) string {
	return fmt.Sprintf("module %s reaches internal symbol %s of module %s at %s:%d — route through %s's facade instead of its internals.",
		from, symbol, to, file, line, to)
}

func msgStaleFacade(mod, symbol string) string {
	return fmt.Sprintf("module %s declares facade entry %s which no longer exists as an export — remove %s from %s's facade or restore the export.",
		mod, symbol, symbol, mod)
}

func msgStaleAllow(mod, dep string) string {
	return fmt.Sprintf("module %s allows dependency %s which is not a governed module or declared layer — fix or remove the entry in %s's dependencies.allow.",
		mod, dep, mod)
}

func msgMissingManifest(mod, dep string) string {
	return fmt.Sprintf("module %s depends on %s, which is an ungoverned module with no grip.yaml — add a grip.yaml to %s so its boundary can be verified, or remove the dependency.",
		mod, dep, dep)
}

func msgCannotVerify(mod, rule, file string, level ir.Level, reason string) string {
	return fmt.Sprintf("cannot verify %s for module %s at %s because analysis confidence is %s (%s) — resolve the dynamic construct or add an explicit declaration so the boundary can be checked.",
		rule, mod, file, level, reason)
}
