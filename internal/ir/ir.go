// Package ir defines the Common Graph IR: the language-neutral contract between
// derivers and the engine. It is the single most important internal artifact —
// every rule, diff, and gate decision reads it, and every current or future
// language deriver (D9) writes it. See plan/01 §4.
//
// Determinism (NFR-1) is structural, not incidental: the IR contains no maps,
// no timestamps, and no absolute paths. Every slice is canonically sorted before
// hashing or output, so a given commit + tool versions hashes byte-identically
// across runs and machines.
package ir

// Version is the IR schema version. Bump it on any breaking schema change so a
// stale golden file fails loudly rather than silently mis-parsing.
const Version = "1"

// Level is an analysis-confidence level for a scope (NFR-9, honest confidence).
// A reduced/none scope that is relevant to a rule flips the gate to a "cannot
// verify — blocked" decision rather than a false pass.
type Level string

const (
	// LevelFull means the analyzer resolved the scope statically and completely.
	LevelFull Level = "full"
	// LevelReduced means part of the scope could not be resolved (dynamic
	// import(), variable-variables, reflection, cross-language calls).
	LevelReduced Level = "reduced"
	// LevelNone means the scope was opaque to the analyzer.
	LevelNone Level = "none"
)

// Rank returns a total order over levels (full < reduced < none) so the lowest
// confidence in a set dominates. Higher rank = less trustworthy.
func (l Level) Rank() int {
	switch l {
	case LevelFull:
		return 0
	case LevelReduced:
		return 1
	case LevelNone:
		return 2
	default:
		// Unknown levels are treated as the least trustworthy (fail-closed).
		return 3
	}
}

// Export is a single symbol a module makes available. Its file:line lets a
// violation point precisely at the offending declaration.
type Export struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // function | class | interface | type | const | method | enum
	File string `json:"file"`
	Line int    `json:"line"`
}

// Module is a governed unit: a directory with a grip.yaml. Its id is the
// repo-relative directory path (D4). Language is per-module so a merged IR can
// hold PHP and TS modules side by side (D2) with no top-level language field.
type Module struct {
	ID       string   `json:"id"`
	Language string   `json:"language"`
	Files    []string `json:"files"`
	// Exports is every symbol the deriver observed the module declare at its
	// top level (the candidate public surface).
	Exports []Export `json:"exports"`
	// ReachableFromOutside is the subset of exported symbol names that are
	// actually referenced from other modules — the real, derived facade.
	ReachableFromOutside []string `json:"reachableFromOutside"`
	// Layer is the architectural layer the deriver could attribute to the
	// module, if any (usually echoed from the manifest by the plane, not the
	// deriver). Empty when the module declares no layer.
	Layer string `json:"layer,omitempty"`
}

// Evidence is one concrete source location backing an edge: which file, which
// line, which symbol crossed the boundary.
type Evidence struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Symbol string `json:"symbol"`
}

// Edge is a directed dependency from one module to another at module
// granularity, carrying the file-level evidence that produced it.
type Edge struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	Kind     string     `json:"kind"` // import | call | extends | implements
	Evidence []Evidence `json:"evidence"`
}

// Confidence records analysis reliability for a scope (a file or module id).
type Confidence struct {
	Scope  string `json:"scope"`
	Level  Level  `json:"level"`
	Reason string `json:"reason"`
}

// Analyzer records a resolved external-tool version. Captured in the IR so that
// "identical commit + tool versions ⇒ byte-identical IR" is enforced by the
// hash: a tool upgrade that changes output changes the hash rather than being
// silently absorbed (NFR-1, plan/01 §3).
type Analyzer struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Language string `json:"language"`
}

// Graph is the whole derived model for a commit: all modules, all module-level
// edges, all confidence records, and the analyzer versions that produced them.
type Graph struct {
	IRVersion  string       `json:"irVersion"`
	Commit     string       `json:"commit"`
	Modules    []Module     `json:"modules"`
	Edges      []Edge       `json:"edges"`
	Confidence []Confidence `json:"confidence"`
	Analyzers  []Analyzer   `json:"analyzers"`
}

// New returns an empty graph stamped with the current IR version.
func New() *Graph {
	return &Graph{IRVersion: Version}
}

// Module returns the module with the given id, or nil if absent.
func (g *Graph) Module(id string) *Module {
	for i := range g.Modules {
		if g.Modules[i].ID == id {
			return &g.Modules[i]
		}
	}
	return nil
}

// ModuleIDs returns the sorted set of module ids in the graph.
func (g *Graph) ModuleIDs() []string {
	ids := make([]string, 0, len(g.Modules))
	for _, m := range g.Modules {
		ids = append(ids, m.ID)
	}
	sortStrings(ids)
	return ids
}
