package contract

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
)

const goAPIHeader = "grip:contract/go-api/v1"

type goAPIElement struct {
	Name      string
	Kind      string
	Signature string
	File      string
	Line      int
}

func deriveGoAPI(mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]*kindVerdict, error) {
	out := map[string]*kindVerdict{}
	refs := append([]plane.ModuleRef(nil), mods...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	for _, mod := range refs {
		current, err := goAPISurface(mod, svc.RepoRoot)
		if err != nil {
			return nil, err
		}
		shape := renderGoAPI(current)
		kv := &kindVerdict{Resolved: true, CurrentShape: shape}
		baselinePath := filepath.Join(mod.Path, filepath.FromSlash(baselineDir), KindAPI+baselineExt)
		baseline, err := os.ReadFile(baselinePath)
		if err == nil {
			prior, parseErr := parseGoAPI(string(baseline))
			if parseErr != nil {
				kv.Resolved = false
				kv.Reason = parseErr.Error()
			} else {
				kv.Changes = diffGoAPI(prior, current)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("contract: read Go API baseline for %s: %w", mod.ID, err)
		}
		out[mod.ID] = kv
	}
	return out, nil
}

func goAPISurface(mod plane.ModuleRef, repoRoot string) (map[string]goAPIElement, error) {
	entries, err := os.ReadDir(mod.Path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	out := map[string]goAPIElement{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		abs := filepath.Join(mod.Path, entry.Name())
		file, err := parser.ParseFile(fset, abs, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("contract: parse %s: %w", abs, err)
		}
		rel, _ := filepath.Rel(repoRoot, abs)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && ast.IsExported(d.Name.Name) {
					out[d.Name.Name] = apiElement(d.Name.Name, "func", nodeText(fset, d.Type), filepath.ToSlash(rel), fset.Position(d.Pos()).Line)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(s.Name.Name) {
							out[s.Name.Name] = apiElement(s.Name.Name, "type", nodeText(fset, s), filepath.ToSlash(rel), fset.Position(s.Pos()).Line)
						}
					case *ast.ValueSpec:
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						for _, name := range s.Names {
							if ast.IsExported(name.Name) {
								out[name.Name] = apiElement(name.Name, kind, nodeText(fset, s), filepath.ToSlash(rel), fset.Position(name.Pos()).Line)
							}
						}
					}
				}
			}
		}
	}
	return out, nil
}

func apiElement(name, kind, signature, file string, line int) goAPIElement {
	return goAPIElement{Name: name, Kind: kind, Signature: strings.Join(strings.Fields(signature), " "), File: file, Line: line}
}

func nodeText(fset *token.FileSet, node any) string {
	var b strings.Builder
	_ = printer.Fprint(&b, fset, node)
	return b.String()
}

func renderGoAPI(elements map[string]goAPIElement) string {
	var names []string
	for name := range elements {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(goAPIHeader + "\n")
	for _, name := range names {
		e := elements[name]
		fmt.Fprintf(&b, "%s\t%s\t%s\n", e.Name, e.Kind, e.Signature)
	}
	return b.String()
}

func parseGoAPI(raw string) (map[string]goAPIElement, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	if !scanner.Scan() || scanner.Text() != goAPIHeader {
		return nil, fmt.Errorf("unrecognized Go API baseline format")
	}
	out := map[string]goAPIElement{}
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		parts := strings.SplitN(scanner.Text(), "\t", 3)
		if len(parts) != 3 || parts[0] == "" {
			return nil, fmt.Errorf("malformed Go API baseline entry %q", scanner.Text())
		}
		out[parts[0]] = goAPIElement{Name: parts[0], Kind: parts[1], Signature: parts[2]}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func diffGoAPI(prior, current map[string]goAPIElement) []Change {
	var changes []Change
	for name, old := range prior {
		now, ok := current[name]
		if !ok {
			changes = append(changes, Change{Nature: NatureRemoved, Element: name, Detail: old.Kind + " removed"})
			continue
		}
		if old.Kind != now.Kind || old.Signature != now.Signature {
			changes = append(changes, Change{Nature: NatureNarrowed, Element: name, Detail: "signature changed", File: now.File, Line: now.Line})
		}
	}
	for name, now := range current {
		if _, ok := prior[name]; !ok {
			changes = append(changes, Change{Nature: NatureOptionalAdded, Element: name, Detail: "exported Go API added", File: now.File, Line: now.Line})
		}
	}
	sortChanges(changes)
	return changes
}

func hasContractArtifacts(svc plane.DeriveServices, extensions []string) bool {
	want := map[string]bool{}
	for _, ext := range extensions {
		want[ext] = true
	}
	for _, spec := range svc.Languages {
		for _, root := range spec.Roots {
			found := false
			_ = filepath.WalkDir(filepath.Join(svc.RepoRoot, root), func(path string, entry os.DirEntry, err error) error {
				if err != nil || found {
					return nil
				}
				if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "node_modules") {
					return filepath.SkipDir
				}
				if !entry.IsDir() && want[strings.ToLower(filepath.Ext(entry.Name()))] {
					found = true
				}
				return nil
			})
			if found {
				return true
			}
		}
	}
	return false
}
