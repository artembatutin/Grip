package ir

import "sort"

// StronglyConnected returns the non-trivial strongly-connected components of the
// module graph (size > 1 — real cycles) via Tarjan's algorithm. Cycle detection
// is Grip's own, computed on the IR, so it is deterministic and tool-independent
// (plan/01 §3). Inputs are sorted before traversal, and members within each
// component are sorted, so output order is stable regardless of edge order
// (NFR-1). Self-loops are ignored (a module depending on itself is not a
// multi-module cycle).
func (g *Graph) StronglyConnected() [][]string {
	nodes := g.ModuleIDs()
	present := map[string]bool{}
	for _, id := range nodes {
		present[id] = true
	}
	adj := map[string][]string{}
	for _, e := range g.Edges {
		if !present[e.From] || !present[e.To] || e.From == e.To {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}
	for id := range adj {
		adj[id] = dedupSorted(adj[id])
	}

	var (
		index   = map[string]int{}
		lowlink = map[string]int{}
		onStack = map[string]bool{}
		stack   []string
		counter int
		out     [][]string
	)
	var strongConnect func(v string)
	strongConnect = func(v string) {
		index[v] = counter
		lowlink[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, seen := index[w]; !seen {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if index[w] < lowlink[v] {
					lowlink[v] = index[w]
				}
			}
		}
		if lowlink[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				out = append(out, comp)
			}
		}
	}
	for _, v := range nodes {
		if _, seen := index[v]; !seen {
			strongConnect(v)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a][0] < out[b][0] })
	return out
}
