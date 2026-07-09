package plane

import (
	"errors"
	"fmt"
	"sort"
)

// ErrToolMissing is returned by ToolRunner when an analyzer required for an
// enabled language is not installed. The gate turns this into a fail-closed
// block (exit 2) with an install remediation, never a silent skip (NFR-6).
var ErrToolMissing = errors.New("required analyzer tool not found")

// ToolMissingError carries the tool name and an install hint for the report.
type ToolMissingError struct {
	Tool string
	Hint string
}

func (e *ToolMissingError) Error() string {
	return fmt.Sprintf("required analyzer %q not found: %s", e.Tool, e.Hint)
}

func (e *ToolMissingError) Unwrap() error { return ErrToolMissing }

// Registry holds the set of planes known to this binary. It is the ONE place
// that knows concrete plane values; the engine core reaches planes only through
// this registry, never by name. Registration order does not affect behavior —
// enabled planes are always processed in a stable, sorted order.
type Registry struct {
	planes map[string]Plane
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{planes: map[string]Plane{}}
}

// Register adds a plane. It panics on a duplicate id, which is always a wiring
// bug (the set of planes is fixed at compile time).
func (r *Registry) Register(p Plane) {
	if _, ok := r.planes[p.ID()]; ok {
		panic(fmt.Sprintf("plane %q registered twice", p.ID()))
	}
	r.planes[p.ID()] = p
}

// Get returns the plane with the given id and whether it exists.
func (r *Registry) Get(id string) (Plane, bool) {
	p, ok := r.planes[id]
	return p, ok
}

// IDs returns the registered plane ids in sorted order (determinism).
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.planes))
	for id := range r.planes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// All returns every registered plane in stable id order.
func (r *Registry) All() []Plane {
	out := make([]Plane, 0, len(r.planes))
	for _, id := range r.IDs() {
		out = append(out, r.planes[id])
	}
	return out
}
