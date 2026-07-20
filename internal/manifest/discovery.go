package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LanguageRoots describes where one language's source lives and which file
// extensions count as its source. Supplied by config; discovery stays generic.
type LanguageRoots struct {
	Language string
	Roots    []string // repo-relative directories
	Exts     []string // file extensions including the dot, e.g. ".ts"
}

// Module is a discovered unit. Governed modules carry a parsed Manifest;
// ungoverned ones do not (FR-14) and are reported distinctly, never silently
// ignored.
type Module struct {
	ID       string // repo-relative directory
	Dir      string // absolute directory
	Language string
	Governed bool
	Manifest *Manifest // nil when ungoverned
}

// Discovery is the deterministic result of walking the configured roots.
type Discovery struct {
	Governed     []*Module
	Ungoverned   []*Module
	fileToModule map[string]string // repo-relative file -> governed module id
	repoRoot     string
}

// Governance rule (documented & tested, plan/03 M0.1):
//   - The nearest-ancestor grip.yaml within a configured language root governs a
//     file. Graph nodes are directories (D4).
//   - A directory that transitively contains source of an enabled language but
//     has no governing grip.yaml at or above it (within the root) is an
//     UNGOVERNED module, reported at the granularity of the immediate child of
//     the root on the path to that source (or the root itself for source that
//     sits directly in the root).

// Discover walks every language root and classifies modules. It is deterministic
// and order-stable: results are sorted by id. A missing root is a fail-closed
// error (a configured root must exist).
func Discover(repoRoot string, langs []LanguageRoots) (*Discovery, error) {
	d := &Discovery{fileToModule: map[string]string{}, repoRoot: repoRoot}

	// Pass 1: find every governed module directory (dir with a grip.yaml) and
	// every source directory, per language.
	governedByLang := map[string]map[string]*Manifest{} // lang -> id -> manifest
	sourceDirs := map[string]map[string]bool{}          // lang -> set of repo-rel dirs holding source

	for _, lg := range langs {
		governedByLang[lg.Language] = map[string]*Manifest{}
		sourceDirs[lg.Language] = map[string]bool{}
		extset := map[string]bool{}
		for _, e := range lg.Exts {
			extset[e] = true
		}
		for _, root := range lg.Roots {
			absRoot := filepath.Join(repoRoot, root)
			info, err := os.Stat(absRoot)
			if err != nil {
				if os.IsNotExist(err) {
					// A configured-but-absent root is tolerated: a repo may not
					// use every language. It contributes no modules.
					continue
				}
				return nil, fmt.Errorf("stat root %s: %w", root, err)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("configured root %s is not a directory", root)
			}
			walkErr := filepath.WalkDir(absRoot, func(p string, e os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if e.IsDir() {
					if isIgnoredDir(e.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if e.Name() == Filename {
					man, lerr := Load(p, repoRoot)
					if lerr != nil {
						return lerr
					}
					governedByLang[lg.Language][man.ID] = man
					return nil
				}
				if isSourceFile(lg.Language, e.Name(), extset) {
					rel, rerr := relID(repoRoot, filepath.Dir(p))
					if rerr != nil {
						return rerr
					}
					sourceDirs[lg.Language][rel] = true
				}
				return nil
			})
			if walkErr != nil {
				return nil, fmt.Errorf("discover under %s: %w", root, walkErr)
			}
		}
	}

	// Pass 2: build governed module list + file mapping and detect ungoverned
	// source directories.
	ungovernedIDs := map[string]string{} // ungoverned id -> language (first seen)
	for _, lg := range langs {
		governed := governedByLang[lg.Language]
		// Sorted governed ids (longest-prefix search wants a stable set).
		gids := make([]string, 0, len(governed))
		for id := range governed {
			gids = append(gids, id)
		}
		sort.Strings(gids)
		// Classify each source directory by nearest governed ancestor.
		active := map[string]bool{}
		for dir := range sourceDirs[lg.Language] {
			owner := nearestAncestor(dir, gids)
			if owner != "" {
				active[owner] = true
			} else {
				uid := immediateChildUnderRoot(dir, lg.Roots)
				if _, ok := ungovernedIDs[uid]; !ok {
					ungovernedIDs[uid] = lg.Language
				}
			}
		}
		// A grip.yaml for another configured language may sit under an
		// overlapping root. It is a module only when it actually owns source for
		// this language; empty/foreign manifests must not become phantom modules.
		for id, man := range governed {
			if !active[id] {
				continue
			}
			d.Governed = append(d.Governed, &Module{
				ID: id, Dir: man.Dir, Language: lg.Language, Governed: true, Manifest: man,
			})
		}
	}

	// File mapping: assign every source file to its nearest governed ancestor.
	for _, lg := range langs {
		governed := governedByLang[lg.Language]
		gids := make([]string, 0, len(governed))
		for id := range governed {
			gids = append(gids, id)
		}
		sort.Strings(gids)
		extset := map[string]bool{}
		for _, e := range lg.Exts {
			extset[e] = true
		}
		for _, root := range lg.Roots {
			absRoot := filepath.Join(repoRoot, root)
			if _, err := os.Stat(absRoot); err != nil {
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
				if !isSourceFile(lg.Language, e.Name(), extset) {
					return nil
				}
				relFile, _ := relID(repoRoot, p)
				relDir, _ := relID(repoRoot, filepath.Dir(p))
				if owner := nearestAncestor(relDir, gids); owner != "" {
					d.fileToModule[relFile] = owner
				}
				return nil
			})
		}
	}

	for uid, lang := range ungovernedIDs {
		d.Ungoverned = append(d.Ungoverned, &Module{
			ID: uid, Dir: filepath.Join(repoRoot, filepath.FromSlash(uid)), Language: lang, Governed: false,
		})
	}

	sort.Slice(d.Governed, func(a, b int) bool { return d.Governed[a].ID < d.Governed[b].ID })
	sort.Slice(d.Ungoverned, func(a, b int) bool { return d.Ungoverned[a].ID < d.Ungoverned[b].ID })
	return d, nil
}

// ModuleForFile returns the governed module id owning a repo-relative file, or
// "" if the file is ungoverned.
func (d *Discovery) ModuleForFile(relFile string) string {
	return d.fileToModule[filepath.ToSlash(relFile)]
}

// UngovernedForFile returns the discovered module without a grip.yaml that owns
// relFile. It is deliberately separate from ModuleForFile so governed graph
// construction cannot accidentally treat an ungoverned boundary as trusted.
func (d *Discovery) UngovernedForFile(relFile string) string {
	for _, m := range d.Ungoverned {
		if relFile == m.ID || strings.HasPrefix(relFile, m.ID+"/") {
			return m.ID
		}
	}
	return ""
}

// GovernedIDs returns governed module ids in sorted order.
func (d *Discovery) GovernedIDs() []string {
	ids := make([]string, 0, len(d.Governed))
	for _, m := range d.Governed {
		ids = append(ids, m.ID)
	}
	return ids
}

// UngovernedIDs returns ungoverned module ids in sorted order.
func (d *Discovery) UngovernedIDs() []string {
	ids := make([]string, 0, len(d.Ungoverned))
	for _, m := range d.Ungoverned {
		ids = append(ids, m.ID)
	}
	return ids
}

// FilesOf returns the sorted repo-relative source files owned by a governed
// module id.
func (d *Discovery) FilesOf(moduleID string) []string {
	var out []string
	for f, id := range d.fileToModule {
		if id == moduleID {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// GovernedModule returns the governed module with the given id, or nil.
func (d *Discovery) GovernedModule(id string) *Module {
	for _, m := range d.Governed {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// nearestAncestor returns the longest governed id that is dir or a prefix
// (ancestor) of dir, or "" if none governs it.
func nearestAncestor(dir string, sortedGovernedIDs []string) string {
	best := ""
	for _, gid := range sortedGovernedIDs {
		if dir == gid || strings.HasPrefix(dir, gid+"/") {
			if len(gid) > len(best) {
				best = gid
			}
		}
	}
	return best
}

// immediateChildUnderRoot returns the immediate child directory of whichever
// root is an ancestor of dir, on the path to dir. If dir IS a root, dir is
// returned (source sits directly in the root).
func immediateChildUnderRoot(dir string, roots []string) string {
	for _, root := range roots {
		r := filepath.ToSlash(root)
		if dir == r {
			return r
		}
		if strings.HasPrefix(dir, r+"/") {
			rest := strings.TrimPrefix(dir, r+"/")
			seg := rest
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				seg = rest[:i]
			}
			return r + "/" + seg
		}
	}
	return dir
}

// isIgnoredDir skips VCS and dependency directories during discovery so we never
// walk node_modules or vendor.
func isIgnoredDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".grip-cache":
		return true
	default:
		return false
	}
}

// isSourceFile excludes Go test-only packages from architecture discovery.
// Tests that live beside production files remain owned by that module, but
// their imports do not create production architecture edges.
func isSourceFile(language, name string, extset map[string]bool) bool {
	if !extset[strings.ToLower(filepath.Ext(name))] {
		return false
	}
	return language != "go" || !strings.HasSuffix(name, "_test.go")
}
