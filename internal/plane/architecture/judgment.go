package architecture

import (
	"context"
	"sort"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// This file implements the Tier C judgment-assisted pass (M4, plan/07 Part A) —
// the ONLY place an LLM enters Grip. It is deliberately hemmed in on every side:
//
//   1. It runs inside Derive (I/O), consults the derived surface read-only, and
//      returns free-text findings into Model.Judgment — a sibling of the graph,
//      NEVER reachable through IRGraph(). No LLM output touches the IR or its hash.
//   2. The plane stamps EVERY judgment violation Tier C, regardless of what the
//      advisor claims, and accepts only the four fixed Tier C rule ids — so the
//      judgment pass cannot label its output as a blocking or promotable rule.
//   3. gate.decide excludes Tier C from the decision structurally, and config
//      refuses to promote a Tier C rule. The LLM therefore cannot change any exit
//      code (principle 3, NFR-1).
//
// Because Tier C is non-deterministic, tests assert only its tier and its
// non-blocking behavior — never its content (plan/07, plan/08).

// Advisor is the judgment seam. An implementation may consult an LLM (or a human,
// or any heuristic) to produce advisory findings from the derived surface. It
// returns findings only; it can never block, and the plane constrains everything
// it emits. The default is noAdvisor — Grip ships deterministic-by-default, and
// the judgment pass is opt-in wiring.
type Advisor interface {
	// Judge inspects the derived graph (read-only) and returns advisory findings.
	// It must not mutate g. It returns no error: a judgment pass that fails simply
	// produces nothing (advisories never fail the gate).
	Judge(ctx context.Context, g *ir.Graph) []JudgmentFinding
}

// JudgmentFinding is one advisory the judgment pass proposes. Its Message is free
// text (non-deterministic); Rule must be one of the Tier C rule ids or the finding
// is dropped.
type JudgmentFinding struct {
	Rule     string
	Location plane.Location
	Message  string
}

// JudgmentSignals is the judgment pass's output, carried beside the graph in the
// Model. Like Signals it is NOT part of IRGraph()/hash.
type JudgmentSignals struct {
	Findings []JudgmentFinding
}

// noAdvisor is the default judgment source: no LLM configured, no findings.
type noAdvisor struct{}

func (noAdvisor) Judge(context.Context, *ir.Graph) []JudgmentFinding { return nil }

// tierCRuleIDs is the fixed, closed set of rule ids the judgment pass may emit. An
// Advisor cannot widen it: a finding naming any other rule id (e.g. a Tier A
// blocking rule) is dropped. This is the plane-level guarantee that an LLM signal
// can never masquerade as a deterministic, gating rule.
var tierCRuleIDs = map[string]bool{
	RuleUnclearName:        true,
	RuleDataClump:          true,
	RulePrimitiveObsession: true,
	RuleFeatureEnvy:        true,
}

// judge runs the advisor and wraps its findings, dropping anything that does not
// name a Tier C rule. It is best-effort — a nil advisor or a nil result yields no
// signals. Output is sorted for stable ordering (the CONTENT is non-deterministic,
// but the ordering is not left to chance).
func judge(ctx context.Context, advisor Advisor, g *ir.Graph) JudgmentSignals {
	if advisor == nil || g == nil {
		return JudgmentSignals{}
	}
	raw := advisor.Judge(ctx, g)
	var kept []JudgmentFinding
	for _, f := range raw {
		if !tierCRuleIDs[f.Rule] {
			continue // the judgment pass may emit ONLY Tier C rules
		}
		kept = append(kept, f)
	}
	sort.SliceStable(kept, func(i, j int) bool { return lessFinding(kept[i], kept[j]) })
	return JudgmentSignals{Findings: kept}
}

func lessFinding(a, b JudgmentFinding) bool {
	if a.Rule != b.Rule {
		return a.Rule < b.Rule
	}
	if a.Location.Module != b.Location.Module {
		return a.Location.Module < b.Location.Module
	}
	if a.Location.File != b.Location.File {
		return a.Location.File < b.Location.File
	}
	return a.Location.Line < b.Location.Line
}

// judgmentViolations turns the (already rule-filtered) judgment signals into Tier C
// violations. Every violation is stamped Tier C here, no matter what — this is the
// load-bearing line that keeps an LLM signal out of the gate. The message is
// carried verbatim from the advisor; the tier/kind/plane are Grip's, not the
// model's.
func judgmentViolations(j JudgmentSignals) []plane.Violation {
	var vs []plane.Violation
	for _, f := range j.Findings {
		if !tierCRuleIDs[f.Rule] {
			continue // defence in depth: emit ONLY the fixed Tier C rule ids
		}
		msg := f.Message
		if msg == "" {
			msg = defaultJudgmentMessage(f.Rule)
		}
		vs = append(vs, plane.Violation{
			RuleID:     f.Rule,
			Plane:      PlaneID,
			Tier:       plane.TierC, // ALWAYS Tier C — never trust the advisor's claim
			Kind:       plane.KindViolation,
			Location:   f.Location,
			Confidence: ir.LevelNone, // a judgment, not a verified fact
			Message:    msg,
		})
	}
	return vs
}

// defaultJudgmentMessage is a fallback sentence when the advisor supplies a rule
// but no message. It keeps every finding a single readable sentence (NFR-5).
func defaultJudgmentMessage(rule string) string {
	switch rule {
	case RuleUnclearName:
		return "a reviewer may find a name here unclear — consider a more descriptive name (judgment-assisted, non-blocking)."
	case RuleDataClump:
		return "these fields recur together and may want their own type (judgment-assisted, non-blocking)."
	case RulePrimitiveObsession:
		return "a primitive here may deserve a domain type (judgment-assisted, non-blocking)."
	case RuleFeatureEnvy:
		return "this code may belong to another module (judgment-assisted, non-blocking)."
	default:
		return "judgment-assisted advisory (non-blocking)."
	}
}
