package contract

import (
	"fmt"

	"github.com/artembatutin/grip/internal/ir"
)

// Stable rule ids — part of Grip's public contract (they appear in reports,
// SARIF, and .grip.yaml promotions), so they change only deliberately. The
// "contract." prefix mirrors "arch.", "test.", and "behavior.": the rule-id
// namespace is per-plane. There is one breaking (Tier A) and one additive (Tier B)
// rule per contract kind, tokenized api/event/db.
const (
	RuleBreakingAPI   = "contract.breaking-api"
	RuleBreakingEvent = "contract.breaking-event"
	RuleBreakingDB    = "contract.breaking-db"
	RuleAdditiveAPI   = "contract.additive-api"
	RuleAdditiveEvent = "contract.additive-event"
	RuleAdditiveDB    = "contract.additive-db"
)

// ruleToken maps a manifest kind id to the token used in rule ids and messages
// (events → "event", to read naturally in "contract.breaking-event").
func ruleToken(kind string) string {
	switch kind {
	case KindEvents:
		return "event"
	default:
		return kind
	}
}

// breakingRule / additiveRule return the stable rule id for a kind.
func breakingRule(kind string) string { return "contract.breaking-" + ruleToken(kind) }
func additiveRule(kind string) string { return "contract.additive-" + ruleToken(kind) }

// kindNoun renders a kind for prose ("api", "event", "db schema").
func kindNoun(kind string) string {
	switch kind {
	case KindAPI:
		return "api"
	case KindEvents:
		return "event"
	case KindDB:
		return "db schema"
	default:
		return kind
	}
}

func breakingSummary(kind string) string {
	return fmt.Sprintf("a backward-incompatible change to the %s contract (removed/renamed in-use element, incompatible migration, or event-shape break) without re-ratification (GR-CON-1)", kindNoun(kind))
}

func additiveSummary(kind string) string {
	return fmt.Sprintf("an additive change or pending deprecation on the %s contract, awaiting consumer sign-off (GR-CON-2)", kindNoun(kind))
}

// Every user-facing string below is one plain sentence: rule, location, and
// remedy (NFR-5), and — the point of this plane — WHAT broke and WHO it breaks.
// They are asserted verbatim in golden tests: a change here is a visible, reviewed
// diff. No timestamps, no absolute paths.

// msgBreaking renders a Tier A breaking change, naming the element, the known
// consumer (when the checker resolved one), and the remedy.
func msgBreaking(mod, kind string, c Change) string {
	return fmt.Sprintf("module %s's %s contract %s%s%s — restore it or run `grip ratify contract %s` to accept the new contract once %s consumers are updated.",
		mod, kindNoun(kind), breakingPhrase(kind, c), consumerClause(c), at(c.File, c.Line), mod, kindNoun(kind))
}

// msgAdditive renders a Tier B advisory (additive element or pending deprecation).
func msgAdditive(mod, kind string, c Change) string {
	return fmt.Sprintf("module %s's %s contract %s%s — run `grip ratify contract %s` to record it as part of the baseline once consumers are ready.",
		mod, kindNoun(kind), additivePhrase(kind, c), at(c.File, c.Line), mod)
}

// msgRepinned renders an intentional re-ratification (principle 5): the human
// changed the declared contract on purpose; it never blocks.
func msgRepinned(mod, kind string) string {
	return fmt.Sprintf("module %s's %s contract was re-ratified on purpose — the baseline now records the new contract shape.", mod, kindNoun(kind))
}

// Fail-closed (cannot-verify) messages: a governed kind whose compatibility we
// cannot establish must never silently pass — it blocks (exit 2) rather than being
// assumed compatible (principle 6, NFR-9).

func msgNoBaseline(mod, kind string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's %s contract because no ratified baseline exists to compare against (confidence %s) — run `grip ratify contract %s` to record the current contract as the baseline.",
		mod, kindNoun(kind), level, mod)
}

func msgUnresolvedPrior(mod, kind, reason string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's %s contract because its prior version could not be resolved (confidence %s) — %s.",
		mod, kindNoun(kind), level, reason)
}

func msgNoVerdict(mod, kind string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's %s contract because its checker returned no verdict for it (confidence %s) — ensure the %s contract checker runs for this module.",
		mod, kindNoun(kind), level, kindNoun(kind))
}

func msgUnknownNature(mod, kind, nature string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's %s contract because its checker reported an unrecognized change kind %q (confidence %s) — upgrade grip or the contract checker so the change can be judged.",
		mod, kindNoun(kind), nature, level)
}

// breakingPhrase renders the verb clause for a breaking change by nature.
func breakingPhrase(kind string, c Change) string {
	switch c.Nature {
	case NatureRemoved:
		return "removes " + c.Element
	case NatureRenamed:
		return "renames " + c.Element
	case NatureRequiredAdded:
		return "adds required " + c.Element
	case NatureNarrowed:
		return "narrows " + c.Element
	case NatureDestructive:
		return "applies a destructive migration to " + c.Element
	default:
		// A nature that reached the breaking path but has no phrase is a wiring bug;
		// render defensively rather than panic.
		return "breaks " + c.Element
	}
}

// additivePhrase renders the verb clause for an additive change by nature.
func additivePhrase(kind string, c Change) string {
	switch c.Nature {
	case NatureOptionalAdded:
		return "adds optional " + c.Element
	case NatureDeprecation:
		return "deprecates " + c.Element
	default:
		return "extends " + c.Element
	}
}

// consumerClause names the known downstream, or "" when the checker could not
// attribute one.
func consumerClause(c Change) string {
	if c.Consumer == "" {
		return ""
	}
	return fmt.Sprintf(", which the %s consumer depends on", c.Consumer)
}

// at renders a " at file:line" suffix when a location is known, else "".
func at(file string, line int) string {
	if file == "" {
		return ""
	}
	return fmt.Sprintf(" at %s:%d", file, line)
}
