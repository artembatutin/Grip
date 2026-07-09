package architecture

import "github.com/artembatutin/grip/internal/ir"

// stronglyConnected returns the module-level cycles in the graph. Cycle
// detection lives on the IR (ir.Graph.StronglyConnected) so the architecture
// plane and the diff share one deterministic implementation.
func stronglyConnected(g *ir.Graph) [][]string { return g.StronglyConnected() }
