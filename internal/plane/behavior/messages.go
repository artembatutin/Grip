package behavior

import (
	"fmt"

	"github.com/artembatutin/grip/internal/ir"
)

// Stable rule ids — part of Grip's public contract (they appear in reports,
// SARIF, and .grip.yaml promotions), so they change only deliberately. The
// "behavior." prefix mirrors "arch." and "test.": the rule-id namespace is
// per-plane.
const (
	// RuleUnratifiedChange (Tier A) — a pinned boundary's observable output
	// changed (or vanished) without an accompanying re-ratification (GR-BEH-1). It
	// also carries the fail-closed cannot-verify results, since those are the same
	// gated boundary whose evidence could not be established.
	RuleUnratifiedChange = "behavior.unratified-change"
	// RuleUnpinnedBoundary (Tier B, promotable) — an observed boundary not yet
	// pinned to a snapshot (GR-BEH-2).
	RuleUnpinnedBoundary = "behavior.unpinned-boundary"
)

// Every user-facing string is one plain sentence: rule, location, and remedy
// (NFR-5). They are asserted verbatim in golden tests — a change here is a
// visible, reviewed diff. No timestamps, no absolute paths: reports stay stable.

func msgUnratifiedDrift(mod, boundary, file string, line int) string {
	return fmt.Sprintf("module %s's boundary %s%s changed its observable output but the pinned snapshot was not re-ratified — restore the behavior or run `grip ratify behavior %s` to pin the new output.",
		mod, boundary, at(file, line), mod)
}

func msgUnratifiedVanished(mod, boundary string) string {
	return fmt.Sprintf("module %s's pinned boundary %s is no longer observed at its facade — restore it or run `grip ratify behavior %s` to drop the stale snapshot.",
		mod, boundary, mod)
}

func msgUnpinned(mod, boundary string) string {
	return fmt.Sprintf("module %s's boundary %s is marked for pinning but has no snapshot yet — run `grip ratify behavior %s` to record its current behavior as the baseline.",
		mod, boundary, mod)
}

func msgNewlyObserved(mod, boundary string) string {
	return fmt.Sprintf("module %s exposes a new observable boundary %s that is not pinned — add it to behavior.pin and run `grip ratify behavior %s` to gate its behavior.",
		mod, boundary, mod)
}

func msgRepinned(mod, boundary string) string {
	return fmt.Sprintf("module %s's boundary %s was re-pinned on purpose — the snapshot now records new observable behavior.",
		mod, boundary)
}

// Fail-closed (cannot-verify) messages: a boundary the human wants gated whose
// evidence we cannot trust must never silently pass — it blocks (exit 2) rather
// than being pinned or assumed unchanged (NFR-9).

func msgNondeterministic(mod, boundary string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's boundary %s because its captured output is nondeterministic (confidence %s) — remove run-to-run noise (time, ordering, random ids) so its snapshot is stable, then pin it.",
		mod, boundary, level)
}

func msgUncaptured(mod, boundary string, level ir.Level) string {
	return fmt.Sprintf("cannot verify module %s's pinned boundary %s because its behavior was not captured this run (confidence %s) — ensure the boundary's tests run so its output can be checked against the snapshot.",
		mod, boundary, level)
}

// at renders a " at file:line" suffix when a location is known, else "".
func at(file string, line int) string {
	if file == "" {
		return ""
	}
	return fmt.Sprintf(" at %s:%d", file, line)
}
