package diff

import (
	"testing"

	"github.com/artembatutin/grip/internal/ir"
)

func g(mods []ir.Module, edges []ir.Edge) *ir.Graph {
	graph := &ir.Graph{IRVersion: ir.Version, Modules: mods, Edges: edges}
	graph.Canonicalize()
	return graph
}

func TestEdgeAndSurfaceDelta(t *testing.T) {
	before := Input{Graph: g(
		[]ir.Module{{ID: "a", ReachableFromOutside: []string{"X"}}, {ID: "b"}},
		[]ir.Edge{{From: "a", To: "b"}},
	)}
	after := Input{Graph: g(
		[]ir.Module{{ID: "a", ReachableFromOutside: []string{"X", "Y"}}, {ID: "b"}, {ID: "c"}},
		nil, // edge a->b removed
	)}
	d := Compute(before, after)
	if len(d.ModulesAdded) != 1 || d.ModulesAdded[0] != "c" {
		t.Errorf("modulesAdded = %v", d.ModulesAdded)
	}
	if len(d.EdgesRemoved) != 1 || d.EdgesRemoved[0] != (EdgeRef{From: "a", To: "b"}) {
		t.Errorf("edgesRemoved = %v", d.EdgesRemoved)
	}
	if len(d.SurfaceWidened) != 1 || d.SurfaceWidened[0].Module != "a" || d.SurfaceWidened[0].Added[0] != "Y" {
		t.Errorf("surfaceWidened = %+v", d.SurfaceWidened)
	}
}

func TestIntentionalFacadeEdit(t *testing.T) {
	graph := g([]ir.Module{{ID: "src/domain", ReachableFromOutside: []string{"Order", "OrderId"}}}, nil)
	before := Input{Graph: graph, Facades: map[string][]string{"src/domain": {"Order"}}}
	after := Input{Graph: graph, Facades: map[string][]string{"src/domain": {"Order", "OrderId"}}}
	d := Compute(before, after)
	if !d.HasIntentional() {
		t.Fatal("facade edit should be intentional")
	}
	if len(d.FacadeEdited) != 1 || d.FacadeEdited[0].Module != "src/domain" || d.FacadeEdited[0].Added[0] != "OrderId" {
		t.Fatalf("facadeEdited = %+v", d.FacadeEdited)
	}
}

func TestCycleDelta(t *testing.T) {
	before := Input{Graph: g([]ir.Module{{ID: "a"}, {ID: "b"}}, []ir.Edge{{From: "a", To: "b"}})}
	after := Input{Graph: g([]ir.Module{{ID: "a"}, {ID: "b"}}, []ir.Edge{{From: "a", To: "b"}, {From: "b", To: "a"}})}
	d := Compute(before, after)
	if len(d.CyclesAdded) != 1 {
		t.Fatalf("cyclesAdded = %v", d.CyclesAdded)
	}
}
