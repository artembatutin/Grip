package ir

import (
	"math/rand"
	"testing"
)

// sampleGraph returns a small graph with intentionally unsorted slices so the
// tests prove canonicalization, not luck.
func sampleGraph() *Graph {
	return &Graph{
		IRVersion: Version,
		Commit:    "abc123",
		Modules: []Module{
			{ID: "src/infrastructure", Language: "typescript",
				Files:   []string{"src/infrastructure/index.ts"},
				Exports: []Export{{Name: "OrderRepo", Kind: "class", File: "src/infrastructure/index.ts", Line: 5}}},
			{ID: "src/domain", Language: "typescript",
				Files:                []string{"src/domain/index.ts"},
				Exports:              []Export{{Name: "Order", Kind: "class", File: "src/domain/index.ts", Line: 2}},
				ReachableFromOutside: []string{"Order"}},
		},
		Edges: []Edge{
			{From: "src/infrastructure", To: "src/domain", Kind: "import",
				Evidence: []Evidence{{File: "src/infrastructure/index.ts", Line: 1, Symbol: "Order"}}},
		},
		Confidence: []Confidence{{Scope: "src/legacy/loader.ts", Level: LevelReduced, Reason: "dynamic import()"}},
		Analyzers:  []Analyzer{{Name: "ts-morph", Version: "22", Language: "typescript"}},
	}
}

// TestHashDeterministicAcrossRuns asserts the IR hash is byte-identical across
// 100 canonicalizations of shuffled copies — the core of NFR-1.
func TestHashDeterministicAcrossRuns(t *testing.T) {
	want := sampleGraph().Hash()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		g := sampleGraph()
		shuffle(g, rng)
		if got := g.Hash(); got != want {
			t.Fatalf("run %d: hash = %s, want %s", i, got, want)
		}
	}
}

// TestRoundTrip asserts canonical JSON parses back to an equal-hashing graph.
func TestRoundTrip(t *testing.T) {
	g := sampleGraph()
	b, err := g.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.Hash() != g.Hash() {
		t.Fatalf("round-trip hash mismatch:\n got %s\nwant %s", back.Hash(), g.Hash())
	}
}

// TestMergeDisjoint merges two single-language graphs and rejects id collisions.
func TestMergeDisjoint(t *testing.T) {
	ts := &Graph{IRVersion: Version, Modules: []Module{{ID: "src/a", Language: "typescript"}}}
	php := &Graph{IRVersion: Version, Modules: []Module{{ID: "app/A", Language: "php"}}}
	merged, err := Merge("c1", ts, php)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Modules) != 2 {
		t.Fatalf("merged modules = %d, want 2", len(merged.Modules))
	}
	if merged.Modules[0].ID != "app/A" { // canonical sort: "app/A" < "src/a"
		t.Fatalf("merged not canonically sorted: %+v", merged.Modules)
	}

	dup := &Graph{IRVersion: Version, Modules: []Module{{ID: "src/a", Language: "php"}}}
	if _, err := Merge("c1", ts, dup); err == nil {
		t.Fatal("expected collision error merging duplicate module id")
	}
}

// TestCycles asserts multi-module cycle detection and that a DAG has none.
func TestCycles(t *testing.T) {
	cyclic := &Graph{IRVersion: Version, Modules: []Module{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}, {From: "b", To: "c"}}}
	comps := cyclic.StronglyConnected()
	if len(comps) != 1 || len(comps[0]) != 2 || comps[0][0] != "a" || comps[0][1] != "b" {
		t.Fatalf("cycle = %v, want [[a b]]", comps)
	}
	dag := &Graph{IRVersion: Version, Modules: []Module{{ID: "a"}, {ID: "b"}},
		Edges: []Edge{{From: "a", To: "b"}}}
	if got := dag.StronglyConnected(); len(got) != 0 {
		t.Fatalf("DAG cycles = %v, want none", got)
	}
}

func shuffle(g *Graph, rng *rand.Rand) {
	rng.Shuffle(len(g.Modules), func(i, j int) { g.Modules[i], g.Modules[j] = g.Modules[j], g.Modules[i] })
	rng.Shuffle(len(g.Edges), func(i, j int) { g.Edges[i], g.Edges[j] = g.Edges[j], g.Edges[i] })
	rng.Shuffle(len(g.Confidence), func(i, j int) { g.Confidence[i], g.Confidence[j] = g.Confidence[j], g.Confidence[i] })
}
