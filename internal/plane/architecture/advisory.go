package architecture

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// This file implements the Tier B advisory derivation (M4, plan/07 Part A). Tier
// B advisories are DETERMINISTIC and non-blocking by default: they are derived by
// wrapping existing analyzers (jscpd-style clone detection, a git-log co-change
// pass, a complexity/AST metrics pass) into normalized signals, exactly as the
// graph deriver wraps dependency-cruiser/deptrac. Crucially the signals live
// beside the graph in the plane's Model and are NEVER folded into the IR — the
// advisory pass cannot change the IR hash or any deterministic gating path.
//
// The derivation is best-effort: an absent or failing advisory tool yields no
// signals and never fails the gate (advisories are non-blocking by nature). The
// deterministic content of a signal, once derived, IS asserted in golden tests —
// only its presence is best-effort, never its bytes.

// advisoryHelper is the logical tool name the ToolRunner resolves for the advisory
// pass. Offline (RecordedRunner) it reads <analysis-dir>/advisory.json; absent, it
// yields an empty report and thus no advisories.
const advisoryHelper = "advisory"

// AdvisoryReport is the normalized output of the wrapped advisory analyzers. Its
// shape mirrors what jscpd (clones), a git-log rollup (co-change), and a
// complexity/AST helper naturally produce, so the helper stays thin. File-located
// facts (clones, message chains, abstractions, complexity) are mapped to modules
// by Grip via ModuleOf; relational facts (co-change, delegation) are emitted
// pre-rolled to module granularity by the helper, which is given the module
// layout — mirroring how the graph helper is given the language roots.
type AdvisoryReport struct {
	Clones        []CloneRec        `json:"clones"`
	CoChanges     []CoChangeRec     `json:"coChanges"`
	Delegations   []DelegationRec   `json:"delegations"`
	MessageChains []MessageChainRec `json:"messageChains"`
	Abstractions  []AbstractionRec  `json:"abstractions"`
	Complexity    []ComplexityRec   `json:"complexity"`
}

// FileLoc is one file:line location in the source.
type FileLoc struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// CloneRec is one duplicated block reported by the clone detector, with every
// occurrence. A block confined to a single module is not a cross-module concern.
type CloneRec struct {
	Lines       int       `json:"lines"`
	Occurrences []FileLoc `json:"occurrences"`
}

// CoChangeRec is a module pair the git-log pass found changing together.
type CoChangeRec struct {
	ModuleA  string `json:"moduleA"`
	ModuleB  string `json:"moduleB"`
	Together int    `json:"together"` // commits touching both modules
	Total    int    `json:"total"`    // commits touching either module
}

// DelegationRec is a module's forwarding profile (middle-man detection).
type DelegationRec struct {
	Module   string `json:"module"`
	Forwards int    `json:"forwards"` // methods that only forward to another module
	Methods  int    `json:"methods"`  // total public methods
}

// MessageChainRec is one navigation chain (a.b().c().d()) at a location.
type MessageChainRec struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Length int    `json:"length"`
}

// AbstractionRec is an interface/abstract type with its implementor count.
type AbstractionRec struct {
	Name         string `json:"name"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Implementors int    `json:"implementors"`
}

// ComplexityRec is one function's cyclomatic complexity.
type ComplexityRec struct {
	Function   string `json:"function"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Complexity int    `json:"complexity"`
}

// --- Resolved signals: the advisory model carried in the plane's Model. Every
// file has been mapped to its governed module, and everything is canonically
// sorted, so the Reconcile step that consumes these stays pure and deterministic.

// Loc is a module-resolved source location.
type Loc struct {
	Module string
	File   string
	Line   int
}

// DuplicationSignal is a clone with its occurrences resolved to governed modules.
type DuplicationSignal struct {
	Lines   int
	Modules []string // distinct governed modules the clone spans (sorted)
	Locs    []Loc    // occurrences in governed files (sorted)
}

// CoChangeSignal is a normalized module pair (A < B) with co-change counts.
type CoChangeSignal struct {
	A, B     string
	Together int
	Total    int
}

// MiddleManSignal is a module's forwarding profile.
type MiddleManSignal struct {
	Module   string
	Forwards int
	Methods  int
}

// ChainSignal is a message chain located in a governed module.
type ChainSignal struct {
	Module string
	File   string
	Line   int
	Length int
}

// AbstractionSignal is an abstraction located in a governed module.
type AbstractionSignal struct {
	Name         string
	Module       string
	File         string
	Line         int
	Implementors int
}

// ComplexitySignal is a function's complexity located in a governed module.
type ComplexitySignal struct {
	Function   string
	Module     string
	File       string
	Line       int
	Complexity int
}

// Signals is the resolved, module-scoped advisory model. It is a sibling of the
// IR graph in the plane's Model and is deliberately NOT reachable through
// IRGraph(), so it never enters the IR hash (NFR-1).
type Signals struct {
	Duplications []DuplicationSignal
	CoChanges    []CoChangeSignal
	MiddleMen    []MiddleManSignal
	Chains       []ChainSignal
	Abstractions []AbstractionSignal
	Complexity   []ComplexitySignal
}

// Empty reports whether there are no advisory signals at all.
func (s Signals) Empty() bool {
	return len(s.Duplications) == 0 && len(s.CoChanges) == 0 && len(s.MiddleMen) == 0 &&
		len(s.Chains) == 0 && len(s.Abstractions) == 0 && len(s.Complexity) == 0
}

// deriveAdvisory runs the advisory pass. It is best-effort: a nil ToolRunner, a
// missing/failed tool, or an unparseable report all yield empty signals and never
// an error — Tier B advisories are non-blocking, so their absence must never
// affect the gate. Determinism of the produced signals is guaranteed by
// normalizeAdvisory (every slice canonically sorted).
func deriveAdvisory(ctx context.Context, svc plane.DeriveServices) Signals {
	if svc.Tools == nil {
		return Signals{}
	}
	out, err := svc.Tools.Run(ctx, advisoryHelper, nil, nil)
	if err != nil {
		return Signals{} // best-effort; advisories never fail the gate
	}
	var rep AdvisoryReport
	if err := json.Unmarshal(out, &rep); err != nil {
		return Signals{}
	}
	return normalizeAdvisory(&rep, svc.ModuleOf)
}

// normalizeAdvisory maps a raw AdvisoryReport into module-resolved Signals. Files
// outside every governed module are dropped (advisories are about governed
// architecture); relational records naming an empty/self module are dropped.
func normalizeAdvisory(rep *AdvisoryReport, moduleOf func(string) string) Signals {
	if moduleOf == nil {
		moduleOf = func(string) string { return "" }
	}
	var s Signals

	for _, c := range rep.Clones {
		modset := map[string]bool{}
		var locs []Loc
		for _, o := range c.Occurrences {
			m := moduleOf(o.File)
			if m == "" {
				continue // ungoverned occurrence; not our concern
			}
			modset[m] = true
			locs = append(locs, Loc{Module: m, File: o.File, Line: o.Line})
		}
		if len(locs) == 0 {
			continue
		}
		sortLocs(locs)
		s.Duplications = append(s.Duplications, DuplicationSignal{Lines: c.Lines, Modules: sortedKeys(modset), Locs: locs})
	}

	for _, r := range rep.CoChanges {
		a, b := r.ModuleA, r.ModuleB
		if a == "" || b == "" || a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		s.CoChanges = append(s.CoChanges, CoChangeSignal{A: a, B: b, Together: r.Together, Total: r.Total})
	}

	for _, r := range rep.Delegations {
		if r.Module == "" {
			continue
		}
		s.MiddleMen = append(s.MiddleMen, MiddleManSignal{Module: r.Module, Forwards: r.Forwards, Methods: r.Methods})
	}

	for _, r := range rep.MessageChains {
		m := moduleOf(r.File)
		if m == "" {
			continue
		}
		s.Chains = append(s.Chains, ChainSignal{Module: m, File: r.File, Line: r.Line, Length: r.Length})
	}

	for _, r := range rep.Abstractions {
		m := moduleOf(r.File)
		if m == "" {
			continue
		}
		s.Abstractions = append(s.Abstractions, AbstractionSignal{Name: r.Name, Module: m, File: r.File, Line: r.Line, Implementors: r.Implementors})
	}

	for _, r := range rep.Complexity {
		m := moduleOf(r.File)
		if m == "" {
			continue
		}
		s.Complexity = append(s.Complexity, ComplexitySignal{Function: r.Function, Module: m, File: r.File, Line: r.Line, Complexity: r.Complexity})
	}

	s.canonicalize()
	return s
}

// canonicalize sorts every signal slice into a stable order so Reconcile output is
// deterministic under any input ordering (NFR-1).
func (s *Signals) canonicalize() {
	sort.SliceStable(s.Duplications, func(i, j int) bool {
		if len(s.Duplications[i].Locs) == 0 || len(s.Duplications[j].Locs) == 0 {
			return len(s.Duplications[i].Locs) < len(s.Duplications[j].Locs)
		}
		return lessLoc(s.Duplications[i].Locs[0], s.Duplications[j].Locs[0])
	})
	sort.SliceStable(s.CoChanges, func(i, j int) bool {
		if s.CoChanges[i].A != s.CoChanges[j].A {
			return s.CoChanges[i].A < s.CoChanges[j].A
		}
		return s.CoChanges[i].B < s.CoChanges[j].B
	})
	sort.SliceStable(s.MiddleMen, func(i, j int) bool { return s.MiddleMen[i].Module < s.MiddleMen[j].Module })
	sort.SliceStable(s.Chains, func(i, j int) bool {
		if s.Chains[i].File != s.Chains[j].File {
			return s.Chains[i].File < s.Chains[j].File
		}
		return s.Chains[i].Line < s.Chains[j].Line
	})
	sort.SliceStable(s.Abstractions, func(i, j int) bool {
		if s.Abstractions[i].File != s.Abstractions[j].File {
			return s.Abstractions[i].File < s.Abstractions[j].File
		}
		if s.Abstractions[i].Line != s.Abstractions[j].Line {
			return s.Abstractions[i].Line < s.Abstractions[j].Line
		}
		return s.Abstractions[i].Name < s.Abstractions[j].Name
	})
	sort.SliceStable(s.Complexity, func(i, j int) bool {
		if s.Complexity[i].File != s.Complexity[j].File {
			return s.Complexity[i].File < s.Complexity[j].File
		}
		return s.Complexity[i].Line < s.Complexity[j].Line
	})
}

func sortLocs(locs []Loc) {
	sort.SliceStable(locs, func(i, j int) bool { return lessLoc(locs[i], locs[j]) })
}

func lessLoc(a, b Loc) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	return a.Line < b.Line
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
