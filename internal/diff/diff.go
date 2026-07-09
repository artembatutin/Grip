// Package diff computes the shape delta between two derived states so the
// architect can approve or reject a structural change in seconds without reading
// implementations (plan/03 M0.7, ACP §5.6). It distinguishes INTENTIONAL changes
// (a manifest edit: facade/allow moved on purpose) from structural drift (edges
// and surface that moved in the code), so an on-purpose facade widening reads as
// "the architect widened this facade", never as a mystery violation (principle
// 5). It is pure: two Inputs in, one Delta out.
package diff

import (
	"sort"

	"github.com/artembatutin/grip/internal/ir"
)

// Input is one side of a comparison: the derived graph plus the declared surface
// (facade) and allowed dependencies per module. Passing the declarations as
// plain data keeps diff engine-generic — it needs no plane knowledge.
type Input struct {
	Graph   *ir.Graph
	Facades map[string][]string // module id -> declared facade
	Allows  map[string][]string // module id -> declared dependencies.allow
}

// EdgeRef names a directed module edge.
type EdgeRef struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SurfaceChange records symbols added to / removed from a module's derived
// external surface.
type SurfaceChange struct {
	Module  string   `json:"module"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// DeclChange records intentional manifest edits (facade or allow) for a module.
type DeclChange struct {
	Module  string   `json:"module"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// Delta is the full shape change between two states.
type Delta struct {
	ModulesAdded    []string        `json:"modulesAdded,omitempty"`
	ModulesRemoved  []string        `json:"modulesRemoved,omitempty"`
	EdgesAdded      []EdgeRef       `json:"edgesAdded,omitempty"`
	EdgesRemoved    []EdgeRef       `json:"edgesRemoved,omitempty"`
	SurfaceWidened  []SurfaceChange `json:"surfaceWidened,omitempty"`
	SurfaceNarrowed []SurfaceChange `json:"surfaceNarrowed,omitempty"`
	FacadeEdited    []DeclChange    `json:"facadeEdited,omitempty"` // intentional
	AllowEdited     []DeclChange    `json:"allowEdited,omitempty"`  // intentional
	CyclesAdded     [][]string      `json:"cyclesAdded,omitempty"`
	CyclesRemoved   [][]string      `json:"cyclesRemoved,omitempty"`
}

// Empty reports whether the delta contains no changes at all.
func (d *Delta) Empty() bool {
	return len(d.ModulesAdded) == 0 && len(d.ModulesRemoved) == 0 &&
		len(d.EdgesAdded) == 0 && len(d.EdgesRemoved) == 0 &&
		len(d.SurfaceWidened) == 0 && len(d.SurfaceNarrowed) == 0 &&
		len(d.FacadeEdited) == 0 && len(d.AllowEdited) == 0 &&
		len(d.CyclesAdded) == 0 && len(d.CyclesRemoved) == 0
}

// HasIntentional reports whether any change came from a manifest edit.
func (d *Delta) HasIntentional() bool {
	return len(d.FacadeEdited) > 0 || len(d.AllowEdited) > 0
}

// Compute returns the shape delta from before to after. All outputs are sorted
// for deterministic rendering.
func Compute(before, after Input) *Delta {
	d := &Delta{}
	bMods := moduleSet(before.Graph)
	aMods := moduleSet(after.Graph)
	d.ModulesAdded = subtract(aMods, bMods)
	d.ModulesRemoved = subtract(bMods, aMods)

	bEdges := edgeSet(before.Graph)
	aEdges := edgeSet(after.Graph)
	for k := range aEdges {
		if !bEdges[k] {
			d.EdgesAdded = append(d.EdgesAdded, k)
		}
	}
	for k := range bEdges {
		if !aEdges[k] {
			d.EdgesRemoved = append(d.EdgesRemoved, k)
		}
	}
	sortEdges(d.EdgesAdded)
	sortEdges(d.EdgesRemoved)

	// Surface changes per module (derived reachable-from-outside).
	for _, id := range union(aMods, bMods) {
		bs := reachable(before.Graph, id)
		as := reachable(after.Graph, id)
		added := subtract(as, bs)
		removed := subtract(bs, as)
		if len(added) > 0 {
			d.SurfaceWidened = append(d.SurfaceWidened, SurfaceChange{Module: id, Added: added})
		}
		if len(removed) > 0 {
			d.SurfaceNarrowed = append(d.SurfaceNarrowed, SurfaceChange{Module: id, Removed: removed})
		}
	}

	// Intentional manifest edits (facade + allow).
	d.FacadeEdited = declDelta(before.Facades, after.Facades)
	d.AllowEdited = declDelta(before.Allows, after.Allows)

	d.CyclesAdded, d.CyclesRemoved = cycleDelta(before.Graph, after.Graph)
	return d
}

func moduleSet(g *ir.Graph) map[string]bool {
	out := map[string]bool{}
	if g == nil {
		return out
	}
	for _, m := range g.Modules {
		out[m.ID] = true
	}
	return out
}

func edgeSet(g *ir.Graph) map[EdgeRef]bool {
	out := map[EdgeRef]bool{}
	if g == nil {
		return out
	}
	for _, e := range g.Edges {
		out[EdgeRef{From: e.From, To: e.To}] = true
	}
	return out
}

func reachable(g *ir.Graph, id string) map[string]bool {
	out := map[string]bool{}
	if g == nil {
		return out
	}
	if m := g.Module(id); m != nil {
		for _, s := range m.ReachableFromOutside {
			out[s] = true
		}
	}
	return out
}

func declDelta(before, after map[string][]string) []DeclChange {
	ids := map[string]bool{}
	for id := range before {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	var order []string
	for id := range ids {
		order = append(order, id)
	}
	sort.Strings(order)
	var out []DeclChange
	for _, id := range order {
		bs := sliceSet(before[id])
		as := sliceSet(after[id])
		added := subtract(as, bs)
		removed := subtract(bs, as)
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		out = append(out, DeclChange{Module: id, Added: added, Removed: removed})
	}
	return out
}

func cycleDelta(before, after *ir.Graph) (added, removed [][]string) {
	b := cycleKeys(before)
	a := cycleKeys(after)
	for k, members := range a {
		if _, ok := b[k]; !ok {
			added = append(added, members)
		}
	}
	for k, members := range b {
		if _, ok := a[k]; !ok {
			removed = append(removed, members)
		}
	}
	sortCycles(added)
	sortCycles(removed)
	return added, removed
}

func cycleKeys(g *ir.Graph) map[string][]string {
	out := map[string][]string{}
	if g == nil {
		return out
	}
	for _, comp := range g.StronglyConnected() {
		key := ""
		for i, m := range comp {
			if i > 0 {
				key += "|"
			}
			key += m
		}
		out[key] = comp
	}
	return out
}

func sliceSet(s []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range s {
		out[v] = true
	}
	return out
}

func subtract(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func union(a, b map[string]bool) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortEdges(e []EdgeRef) {
	sort.Slice(e, func(a, b int) bool {
		if e[a].From != e[b].From {
			return e[a].From < e[b].From
		}
		return e[a].To < e[b].To
	})
}

func sortCycles(c [][]string) {
	for i := range c {
		sort.Strings(c[i])
	}
	sort.Slice(c, func(a, b int) bool {
		if len(c[a]) == 0 || len(c[b]) == 0 {
			return len(c[a]) < len(c[b])
		}
		return c[a][0] < c[b][0]
	})
}
