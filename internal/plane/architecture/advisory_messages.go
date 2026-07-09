package architecture

import (
	"fmt"
	"strings"
)

// Tier B advisory messages. Like the Tier A messages, each is one plain sentence
// naming the rule, the location, and the remedy (NFR-5), and each is asserted in
// golden tests — a change here is a visible, reviewed diff. Tier B is deterministic
// (unlike Tier C), so pinning the exact wording is correct.

func msgDuplication(d DuplicationSignal) string {
	return fmt.Sprintf("modules %s share %d lines of duplicated code (e.g. %s and %s) — extract the shared logic into one module both depend on.",
		strings.Join(d.Modules, ", "), d.Lines, locString(d.Locs[0]), locString(d.Locs[len(d.Locs)-1]))
}

func msgCoChange(c CoChangeSignal) string {
	return fmt.Sprintf("modules %s and %s changed together in %d of %d commits but neither declares a dependency on the other — make the coupling explicit in a grip.yaml or decouple them.",
		c.A, c.B, c.Together, c.Total)
}

func msgMiddleMan(m MiddleManSignal) string {
	return fmt.Sprintf("module %s forwards %d of its %d methods to other modules — it may be a middle man; inline it or give it behavior of its own.",
		m.Module, m.Forwards, m.Methods)
}

func msgMessageChain(ch ChainSignal) string {
	return fmt.Sprintf("a message chain of length %d at %s:%d reaches across module boundaries — add a method on the first object so callers do not navigate its internals.",
		ch.Length, ch.File, ch.Line)
}

func msgSpeculative(a AbstractionSignal) string {
	return fmt.Sprintf("abstraction %s in module %s at %s:%d has a single implementor — it may be speculative generality; remove the indirection until a second implementor exists.",
		a.Name, a.Module, a.File, a.Line)
}

func msgComplexity(c ComplexitySignal) string {
	return fmt.Sprintf("function %s at %s:%d has cyclomatic complexity %d (advisory threshold %d) — break it into smaller functions.",
		c.Function, c.File, c.Line, c.Complexity, complexityThreshold)
}

func locString(l Loc) string {
	return fmt.Sprintf("%s:%d", l.File, l.Line)
}
