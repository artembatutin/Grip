package manifest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
)

// Candidate is a provisional module proposed during onboarding, before any
// grip.yaml exists. Its id is the immediate child directory of a language root
// that (transitively) contains source — the conventional module granularity.
type Candidate struct {
	ID       string
	Dir      string
	Language string
	Files    []string
}

// CandidateSet is the result of proposing modules for a repo with no manifests
// yet (plan/03 M0.10). It provides the same ModuleOf/FilesOf services the gate
// gives a plane, so the same deriver runs during `grip init`.
type CandidateSet struct {
	modules      []Candidate
	fileToModule map[string]string
}

// Candidates proposes modules from source layout alone: each immediate child
// directory of a language root that contains source of that language becomes a
// candidate module. This is what lets onboarding derive structure before the
// human has written a single grip.yaml.
func Candidates(repoRoot string, langs []LanguageRoots) (*CandidateSet, error) {
	cs := &CandidateSet{fileToModule: map[string]string{}}
	byID := map[string]*Candidate{}

	for _, lg := range langs {
		extset := map[string]bool{}
		for _, e := range lg.Exts {
			extset[e] = true
		}
		for _, root := range lg.Roots {
			absRoot := filepath.Join(repoRoot, root)
			if info, err := os.Stat(absRoot); err != nil || !info.IsDir() {
				continue
			}
			_ = filepath.WalkDir(absRoot, func(p string, e os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if e.IsDir() {
					if isIgnoredDir(e.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if !extset[strings.ToLower(filepath.Ext(e.Name()))] {
					return nil
				}
				relFile, _ := relID(repoRoot, p)
				relDir, _ := relID(repoRoot, filepath.Dir(p))
				id := immediateChildUnderRoot(relDir, lg.Roots)
				c := byID[id]
				if c == nil {
					c = &Candidate{ID: id, Dir: filepath.Join(repoRoot, filepath.FromSlash(id)), Language: lg.Language}
					byID[id] = c
				}
				c.Files = append(c.Files, relFile)
				cs.fileToModule[relFile] = id
				return nil
			})
		}
	}
	for _, c := range byID {
		sort.Strings(c.Files)
		cs.modules = append(cs.modules, *c)
	}
	sort.Slice(cs.modules, func(a, b int) bool { return cs.modules[a].ID < cs.modules[b].ID })
	return cs, nil
}

// Refs returns the candidate modules as plane.ModuleRef values for the deriver.
func (c *CandidateSet) Refs() []plane.ModuleRef {
	out := make([]plane.ModuleRef, 0, len(c.modules))
	for _, m := range c.modules {
		out = append(out, plane.ModuleRef{ID: m.ID, Path: m.Dir, Language: m.Language})
	}
	return out
}

// ModuleOf returns the candidate module id owning a repo-relative file, or "".
func (c *CandidateSet) ModuleOf(relFile string) string {
	return c.fileToModule[filepath.ToSlash(relFile)]
}

// FilesOf returns the files of a candidate module id.
func (c *CandidateSet) FilesOf(id string) []string {
	for _, m := range c.modules {
		if m.ID == id {
			return append([]string(nil), m.Files...)
		}
	}
	return nil
}
