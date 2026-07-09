package architecture

import (
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Advisory thresholds. Each is a fixed, documented policy so the boundary between
// "fires" and "near-miss" is deterministic and unit-testable. They are rule
// policy, so they live here in the reconcile step (pure), not in Derive.
const (
	minCloneLines         = 5  // clones shorter than this are noise
	coChangeMinCommits    = 3  // ignore pairs that co-changed only once or twice
	coChangeRatioNum      = 3  // fire when together/total >= 3/4 (75%)
	coChangeRatioDen      = 4  //
	middleManMinMethods   = 4  // ignore tiny modules
	middleManRatioNum     = 3  // fire when forwards/methods >= 3/4 (75%)
	middleManRatioDen     = 4  //
	messageChainThreshold = 4  // chain length at/above this is a chain
	complexityThreshold   = 10 // cyclomatic complexity strictly above this breaches
)

// advisoryViolations is the pure Tier B rule set: it turns resolved advisory
// Signals into non-blocking Tier B violations, applying each rule's threshold and,
// for co-change, the declared-dependency check. It reads intents (for declared
// dependencies) but does no I/O and no map-order-dependent work — the caller
// passes canonically sorted Signals, so output is deterministic (NFR-1).
//
// Every violation here is Tier B: reported, and non-blocking unless the repo
// promotes the rule in .grip.yaml (gate.decide + config, already wired). The
// message is always one plain sentence naming the rule, the location, and the
// remedy (NFR-5).
func advisoryViolations(intents map[string]Intent, s Signals) []plane.Violation {
	var vs []plane.Violation

	// cross-module duplication: a clone spanning >= 2 distinct governed modules.
	for _, d := range s.Duplications {
		if len(d.Modules) < 2 || d.Lines < minCloneLines {
			continue // single-module clone or too short — not a cross-module concern
		}
		vs = append(vs, advisory(RuleDuplication, locOf(d.Locs[0], ""), msgDuplication(d)))
	}

	// co-change coupling: two governed modules that change together often but
	// declare no dependency on each other (an implicit, hidden coupling).
	for _, c := range s.CoChanges {
		if c.Together < coChangeMinCommits {
			continue
		}
		if c.Together*coChangeRatioDen < c.Total*coChangeRatioNum {
			continue // below the coupling ratio
		}
		if !bothGoverned(c.A, c.B, intents) {
			continue
		}
		if declaredDependency(c.A, c.B, intents) {
			continue // the coupling is already explicit — nothing to advise
		}
		vs = append(vs, advisory(RuleCoChange, plane.Location{Module: c.A}, msgCoChange(c)))
	}

	// middle man / excessive delegation: a module that mostly forwards calls.
	for _, m := range s.MiddleMen {
		if m.Methods < middleManMinMethods {
			continue
		}
		if m.Forwards*middleManRatioDen < m.Methods*middleManRatioNum {
			continue
		}
		vs = append(vs, advisory(RuleMiddleMan, plane.Location{Module: m.Module}, msgMiddleMan(m)))
	}

	// message chains: a long navigation chain reaching across boundaries.
	for _, ch := range s.Chains {
		if ch.Length < messageChainThreshold {
			continue
		}
		vs = append(vs, advisory(RuleMessageChains, plane.Location{Module: ch.Module, File: ch.File, Line: ch.Line}, msgMessageChain(ch)))
	}

	// speculative generality: an abstraction with a single implementor.
	for _, a := range s.Abstractions {
		if a.Implementors > 1 {
			continue
		}
		vs = append(vs, advisory(RuleSpeculativeGenerality, plane.Location{Module: a.Module, File: a.File, Line: a.Line, Symbol: a.Name}, msgSpeculative(a)))
	}

	// complexity breach: a function above the cyclomatic-complexity threshold.
	for _, c := range s.Complexity {
		if c.Complexity <= complexityThreshold {
			continue
		}
		vs = append(vs, advisory(RuleComplexity, plane.Location{Module: c.Module, File: c.File, Line: c.Line, Symbol: c.Function}, msgComplexity(c)))
	}

	return vs
}

// advisory builds a Tier B violation with full confidence. Tier B is non-blocking
// by default; the gate blocks on it only if the repo promoted the rule.
func advisory(rule string, loc plane.Location, msg string) plane.Violation {
	return plane.Violation{
		RuleID:     rule,
		Plane:      PlaneID,
		Tier:       plane.TierB,
		Kind:       plane.KindViolation,
		Location:   loc,
		Confidence: ir.LevelFull,
		Message:    msg,
	}
}

func locOf(l Loc, symbol string) plane.Location {
	return plane.Location{Module: l.Module, File: l.File, Line: l.Line, Symbol: symbol}
}

func bothGoverned(a, b string, intents map[string]Intent) bool {
	_, oka := intents[a]
	_, okb := intents[b]
	return oka && okb
}

// declaredDependency reports whether either module allows the other in its
// architecture.dependencies.allow — i.e. the coupling is already explicit.
func declaredDependency(a, b string, intents map[string]Intent) bool {
	return allowsModule(intents[a], b) || allowsModule(intents[b], a)
}

func allowsModule(in Intent, dep string) bool {
	for _, e := range in.Allow {
		if e == dep {
			return true
		}
	}
	return false
}
