package contract

import (
	"path"
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// BaselineFile is one ratified baseline to (re-)write: the repo-relative path it
// belongs at and the exact canonical current contract shape to record. A kind
// whose checker produced no current shape is returned with Missing=true and empty
// Content so the caller refuses to adopt it — never ratify a contract Grip could
// not derive (fail-closed).
type BaselineFile struct {
	// Path is repo-relative, e.g. "src/checkout/.grip/contract/api.contract".
	Path    string
	Kind    string
	Content string
	Missing bool
}

// BaselinesFor returns the baseline files that `grip ratify contract <module>`
// should write to adopt a module's currently-derived contract as the declared
// baseline (reusing generate-then-ratify, M0.10). This IS the recorded design
// decision (principle 5): the committed baseline artifact is what a later gate
// reads as the ratification, and what the report renders as an intentional change.
//
// filter, when non-nil, restricts adoption to the named kinds (the module's
// governed `contract:` kinds). A nil filter adopts every kind whose checker
// produced a current shape. Output is sorted by kind for stable CLI reporting.
func BaselinesFor(derived plane.Derived, moduleID string, filter map[string]bool) []BaselineFile {
	m, ok := derived.(*Model)
	if !ok || m == nil {
		return nil
	}
	st := m.Module(moduleID)
	if st == nil {
		return nil
	}
	var out []BaselineFile
	for _, kind := range kindsInOrder {
		if filter != nil && !filter[kind] {
			continue
		}
		ks := st.Kind(kind)
		if ks == nil || !ks.HasVerdict {
			continue // no checker verdict → nothing current to adopt
		}
		f := BaselineFile{
			Path: path.Join(moduleID, baselineDir, kind+baselineExt),
			Kind: kind,
		}
		if ks.CurrentShape == "" {
			f.Missing = true
		} else {
			f.Content = ks.CurrentShape
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}
