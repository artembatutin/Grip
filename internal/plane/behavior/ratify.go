package behavior

import (
	"path"
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// SnapFile is one snapshot to (re-)pin: the repo-relative path it belongs at and
// the exact canonical content to write. A boundary whose capture is
// nondeterministic is returned with Reduced=true and empty Content so the caller
// refuses to pin it — never pin an unstable snapshot (fail-closed).
type SnapFile struct {
	// Path is repo-relative, e.g. "src/checkout/.grip/behavior/placeOrder.snap".
	Path     string
	Boundary string
	Content  string
	Reduced  bool
}

// SnapshotsFor returns the snapshot files that `grip ratify behavior <module>`
// should write to pin a module's currently-observed boundaries. This IS the
// recorded design decision (principle 5): the edited snapshot file, committed
// alongside the code, is what a later gate reads as the ratification, and what the
// report renders as an intentional change.
//
// filter, when non-nil, restricts pinning to the named boundaries (the module's
// behavior.pin). A nil filter pins every observed boundary — "accept current
// reality", the declare-nothing path. Output is sorted by boundary for stable CLI
// reporting.
func SnapshotsFor(derived plane.Derived, moduleID string, filter map[string]bool) []SnapFile {
	m, ok := derived.(*Model)
	if !ok || m == nil {
		return nil
	}
	st := m.Module(moduleID)
	if st == nil {
		return nil
	}
	var out []SnapFile
	for _, b := range st.Boundaries {
		if !b.Observed {
			continue // only reality can be pinned
		}
		if filter != nil && !filter[b.Name] {
			continue
		}
		f := SnapFile{
			Path:     path.Join(moduleID, snapshotDir, b.Name+snapshotExt),
			Boundary: b.Name,
		}
		if b.Reduced {
			f.Reduced = true
		} else {
			f.Content = b.DerivedSnapshot
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Boundary < out[j].Boundary })
	return out
}
