package contract

// This file is the pure policy core: it maps one atomic, policy-NEUTRAL change
// (as a wrapped breaking-change checker reports it) to a verdict UNDER a module's
// declared compatibility policy. Keeping the policy here — not in the checker — is
// what makes the reconcile a genuine per-policy decision (NFR-8: wrap the differ,
// own the judgment) and what makes it mutation-testable in isolation: flip any
// cell and a reconcile test must fail.

// Nature is the policy-neutral classification a checker attaches to one change.
// It is a CLOSED set; a nature Grip does not recognize is fail-closed (the checker
// saw something we cannot judge, so we must not pass it). Checkers emit these; the
// policy table below — not the checker — decides whether each breaks.
type Nature string

const (
	// NatureRemoved: an in-use element (field, endpoint, message field, column)
	// was removed. Breaks a consumer under every policy.
	NatureRemoved Nature = "removed"
	// NatureRenamed: an element was renamed (= removed + added). Breaks under every
	// policy; the checker names both the old and new element.
	NatureRenamed Nature = "renamed"
	// NatureRequiredAdded: a new REQUIRED input/element was added. Breaks backward
	// (old consumers omit it) and full; safe forward.
	NatureRequiredAdded Nature = "required-added"
	// NatureNarrowed: a type or constraint was tightened. Breaks backward and full;
	// safe forward.
	NatureNarrowed Nature = "narrowed"
	// NatureDestructive: a DB migration that drops/rewrites data or adds a
	// non-nullable column without a default. Breaks under every policy.
	NatureDestructive Nature = "destructive"
	// NatureOptionalAdded: a new OPTIONAL element was added. Additive under every
	// policy (Tier B advisory, never a block).
	NatureOptionalAdded Nature = "optional-added"
	// NatureWidened: a type or constraint was loosened. Compatible under every
	// policy — accepted silently.
	NatureWidened Nature = "widened"
	// NatureDeprecation: an element was marked deprecated but not yet removed.
	// Additive/pending under every policy (GR-CON-2; Tier B until consumers sign off).
	NatureDeprecation Nature = "deprecation"
)

// Verdict is the outcome of applying a policy to one change.
type Verdict int

const (
	// VerdictCompatible: the change is safe under the policy — no report.
	VerdictCompatible Verdict = iota
	// VerdictAdditive: the change is backward-safe but widens the contract — a Tier
	// B advisory (a pending deprecation or a new optional element).
	VerdictAdditive
	// VerdictBreaking: the change is incompatible under the policy — a Tier A block.
	VerdictBreaking
)

// classify maps (nature, policy) to a verdict. The second return is false for a
// nature Grip does not recognize — the caller MUST treat that as cannot-verify
// (fail-closed), never as compatible: an unrecognized change is an unjudged change.
//
// The table is intentionally conservative: when a direction is ambiguous it favors
// "breaking" over a false pass. The load-bearing rows for the exit criteria are
// removed/renamed/destructive (break everywhere) and required-added/narrowed
// (break under backward & full, safe under forward — the policy near-miss).
func classify(nature Nature, compat Compat) (Verdict, bool) {
	switch nature {
	case NatureRemoved, NatureRenamed, NatureDestructive:
		return VerdictBreaking, true // an in-use removal/rewrite breaks every direction
	case NatureRequiredAdded, NatureNarrowed:
		if compat == CompatForward {
			return VerdictCompatible, true // old producer ignores the new/tighter input
		}
		return VerdictBreaking, true // backward & full: old consumers can no longer comply
	case NatureOptionalAdded, NatureDeprecation:
		return VerdictAdditive, true // widens the surface; advisory only (GR-CON-2)
	case NatureWidened:
		return VerdictCompatible, true // loosening never breaks either direction
	default:
		return VerdictCompatible, false // unknown nature → fail-closed at the caller
	}
}
