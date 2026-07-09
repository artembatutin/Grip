package architecture

import (
	"sort"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Model is the architecture plane's Derived value: the Common Graph IR, the
// repo's layer order, the set of ungoverned module ids, and the Tier B advisory
// signals. Bundling these here (captured in Derive) keeps Reconcile pure — it
// needs no config and no I/O.
//
// Advisory is deliberately a sibling of Graph, NOT reachable through IRGraph(): the
// advisory pass (and, in M4, the Tier C judgment pass) must never enter the IR or
// its hash. IRGraph() returns only Graph, so the deterministic gate path is
// hermetically isolated from advisory output (NFR-1, principle 3).
type Model struct {
	Graph      *ir.Graph
	LayerOrder []string
	Ungoverned []string
	Advisory   Signals
	Judgment   JudgmentSignals
}

// IRGraph exposes the Common Graph IR so the engine can surface it for
// diff/report/version without importing this plane (satisfies gate.GraphProvider).
// It returns ONLY the graph — never the advisory or judgment signals — so nothing
// derived from a heuristic or an LLM can reach the IR hash.
func (m *Model) IRGraph() *ir.Graph { return m.Graph }

// reconcile is the pure heart of the plane: (declared intents, derived model) →
// located, one-sentence violations. No I/O, no clock, no map-iteration-order
// leaks — every loop is over a sorted key set so output is deterministic under
// shuffled inputs (NFR-1, principle 3).
func reconcile(intents map[string]Intent, m *Model) []plane.Violation {
	g := m.Graph
	var vs []plane.Violation

	exportsByMod := indexExports(g)             // modID -> symbol -> Export
	exportNames := exportNameSets(exportsByMod) // modID -> set of exported names
	layerIdx := layerIndex(m.LayerOrder)
	ungoverned := stringSet(m.Ungoverned)

	// Sorted module id list for deterministic iteration.
	modIDs := make([]string, 0, len(intents))
	for id := range intents {
		modIDs = append(modIDs, id)
	}
	sort.Strings(modIDs)

	// --- Per-edge rules: illegal-dependency, direction-violation, internal-reach.
	// g.Edges is canonically sorted by (from,to,kind).
	for i := range g.Edges {
		e := g.Edges[i]
		fromIntent, fromGoverned := intents[e.From]
		toIntent, toGoverned := intents[e.To]
		if !fromGoverned || !toGoverned {
			continue // edges exist only between governed modules; defensive.
		}
		loc := plane.Location{Module: e.From}
		if len(e.Evidence) > 0 {
			loc.File = e.Evidence[0].File
			loc.Line = e.Evidence[0].Line
			loc.Symbol = e.Evidence[0].Symbol
		}

		// illegal-dependency (FR-3).
		if !isAllowed(fromIntent, e.To, toIntent.Layer) {
			vs = append(vs, plane.Violation{
				RuleID: RuleIllegalDependency, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
				Location: loc, Confidence: ir.LevelFull,
				Message: msgIllegalDependency(e.From, e.To, loc.File, loc.Line),
			})
		}

		// direction-violation (FR-5): a dependency pointing outward across layers.
		if fromIntent.Layer != "" && toIntent.Layer != "" {
			fi, fok := layerIdx[fromIntent.Layer]
			ti, tok := layerIdx[toIntent.Layer]
			if fok && tok && fi < ti {
				vs = append(vs, plane.Violation{
					RuleID: RuleDirectionViolation, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
					Location: loc, Confidence: ir.LevelFull,
					Message: msgDirectionViolation(e.From, fromIntent.Layer, e.To, toIntent.Layer, loc.File, loc.Line, m.LayerOrder),
				})
			}
		}

		// internal-reach (FR-8): the edge targets a symbol that is not part of
		// the target's entrypoint surface (reached past the facade).
		toExports := exportNames[e.To]
		for _, ev := range e.Evidence {
			if ev.Symbol == "" {
				continue
			}
			if !toExports[ev.Symbol] {
				vs = append(vs, plane.Violation{
					RuleID: RuleInternalReach, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
					Location:   plane.Location{Module: e.From, File: ev.File, Line: ev.Line, Symbol: ev.Symbol},
					Confidence: ir.LevelFull,
					Message:    msgInternalReach(e.From, e.To, ev.Symbol, ev.File, ev.Line),
				})
			}
		}
	}

	// --- Per-module rules: facade-widening, stale-declaration.
	for _, id := range modIDs {
		intent := intents[id]
		mod := g.Module(id)
		if mod == nil {
			continue
		}
		facade := stringSet(intent.Facade)
		names := exportNames[id]

		// facade-widening (FR-4): a genuinely exported symbol used from outside
		// but absent from the declared facade.
		reached := append([]string(nil), mod.ReachableFromOutside...)
		sort.Strings(reached)
		for _, s := range reached {
			if facade[s] || !names[s] {
				continue // in facade already, or not an entrypoint export (that's internal-reach's job)
			}
			ex := exportsByMod[id][s]
			vs = append(vs, plane.Violation{
				RuleID: RuleFacadeWidening, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
				Location:   plane.Location{Module: id, File: ex.File, Line: ex.Line, Symbol: s},
				Confidence: ir.LevelFull,
				Message:    msgFacadeWidening(id, s, ex.File, ex.Line),
			})
		}

		// stale-declaration (FR-6), facade side: a facade entry with no backing
		// export (the export was deleted or renamed).
		facadeEntries := append([]string(nil), intent.Facade...)
		sort.Strings(facadeEntries)
		for _, f := range facadeEntries {
			if !names[f] {
				vs = append(vs, plane.Violation{
					RuleID: RuleStaleDeclaration, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindStaleDeclaration,
					Location:   plane.Location{Module: id, Symbol: f},
					Confidence: ir.LevelFull,
					Message:    msgStaleFacade(id, f),
				})
			}
		}

		// allow-side classification of an entry that names neither a governed
		// module nor a declared layer:
		//   - it names an ungoverned module (manifest missing) → fail-closed
		//     "cannot verify" (the boundary of a referenced module is unknown).
		//   - it names nothing at all (a phantom) → stale-declaration.
		allowEntries := append([]string(nil), intent.Allow...)
		sort.Strings(allowEntries)
		for _, a := range allowEntries {
			if _, governed := intents[a]; governed {
				continue
			}
			if _, isLayer := layerIdx[a]; isLayer {
				continue
			}
			if ungoverned[a] {
				vs = append(vs, plane.Violation{
					RuleID: RuleIllegalDependency, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
					Location:   plane.Location{Module: id, Symbol: a},
					Confidence: ir.LevelNone,
					Message:    msgMissingManifest(id, a),
				})
				continue
			}
			vs = append(vs, plane.Violation{
				RuleID: RuleStaleDeclaration, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindStaleDeclaration,
				Location:   plane.Location{Module: id, Symbol: a},
				Confidence: ir.LevelFull,
				Message:    msgStaleAllow(id, a),
			})
		}
	}

	// --- cycle (FR-5): non-trivial strongly-connected components.
	for _, comp := range stronglyConnected(g) {
		// All members are governed modules by construction; report on the first.
		vs = append(vs, plane.Violation{
			RuleID: RuleCycle, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
			Location:   plane.Location{Module: comp[0]},
			Confidence: ir.LevelFull,
			Message:    msgCycle(comp),
		})
	}

	// --- cannot-verify (NFR-9 fail-closed): a governed module contains a
	// reduced/none-confidence scope, so an illegal edge could be hidden.
	for _, c := range sortedConfidence(g) {
		if c.Level != ir.LevelReduced && c.Level != ir.LevelNone {
			continue
		}
		owner := ownerOf(c.Scope, modIDs)
		if owner == "" {
			continue // reduced scope outside any governed module; nothing to gate
		}
		vs = append(vs, plane.Violation{
			RuleID: RuleIllegalDependency, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
			Location:   plane.Location{Module: owner, File: c.Scope},
			Confidence: c.Level,
			Message:    msgCannotVerify(owner, "the dependency boundary", c.Scope, c.Level, c.Reason),
		})
	}

	// --- Tier B advisories (M4): deterministic, non-blocking by default. Derived
	// from the wrapped advisory analyzers into m.Advisory in Derive; turned into
	// reported-but-non-gating violations here. They share this pure step but never
	// the IR — m.Advisory is not part of the graph.
	vs = append(vs, advisoryViolations(intents, m.Advisory)...)

	// --- Tier C judgment (M4): non-deterministic, judgment-assisted, ALWAYS
	// non-blocking. Produced by the injected advisor in Derive (the only LLM entry
	// point) into m.Judgment; stamped Tier C here so it can never gate a merge.
	vs = append(vs, judgmentViolations(m.Judgment)...)

	return vs
}

func isAllowed(from Intent, toMod, toLayer string) bool {
	for _, e := range from.Allow {
		if e == toMod {
			return true
		}
		if toLayer != "" && e == toLayer {
			return true
		}
	}
	return false
}

func indexExports(g *ir.Graph) map[string]map[string]ir.Export {
	out := map[string]map[string]ir.Export{}
	for _, m := range g.Modules {
		byName := map[string]ir.Export{}
		for _, ex := range m.Exports {
			if _, ok := byName[ex.Name]; !ok {
				byName[ex.Name] = ex
			}
		}
		out[m.ID] = byName
	}
	return out
}

func exportNameSets(idx map[string]map[string]ir.Export) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for id, byName := range idx {
		set := map[string]bool{}
		for name := range byName {
			set[name] = true
		}
		out[id] = set
	}
	return out
}

func layerIndex(order []string) map[string]int {
	out := map[string]int{}
	for i, l := range order {
		out[l] = i
	}
	return out
}

func stringSet(s []string) map[string]bool {
	out := make(map[string]bool, len(s))
	for _, v := range s {
		out[v] = true
	}
	return out
}

// ownerOf returns the governed module id that owns a confidence scope: the
// longest module id equal to the scope or an ancestor directory of it. "" when
// the scope falls outside every governed module.
func ownerOf(scope string, modIDs []string) string {
	best := ""
	for _, id := range modIDs {
		if scope == id || hasPathPrefix(scope, id) {
			if len(id) > len(best) {
				best = id
			}
		}
	}
	return best
}

func hasPathPrefix(path, prefix string) bool {
	return len(path) > len(prefix) && path[:len(prefix)] == prefix && path[len(prefix)] == '/'
}

func sortedConfidence(g *ir.Graph) []ir.Confidence {
	out := append([]ir.Confidence(nil), g.Confidence...)
	sort.Slice(out, func(a, b int) bool {
		if out[a].Scope != out[b].Scope {
			return out[a].Scope < out[b].Scope
		}
		return out[a].Reason < out[b].Reason
	})
	return out
}
