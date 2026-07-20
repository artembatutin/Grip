package behavior

import (
	"context"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
)

// captureGo treats verified Go examples as observable package boundaries. The
// Go test runner executes each example and independently checks its Output
// directive; Grip snapshots that verified output so a code+test rewrite cannot
// silently redefine behavior without a re-pin.
func captureGo(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]map[string]capturedBoundary, error) {
	patterns := goTestPatterns(svc)
	if _, err := svc.Tools.Run(ctx, "go", append([]string{"test", "-count=1"}, patterns...), nil); err != nil {
		return nil, fmt.Errorf("behavior: Go boundary examples failed: %w", err)
	}
	out := map[string]map[string]capturedBoundary{}
	refs := append([]plane.ModuleRef(nil), mods...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	for _, mod := range refs {
		files, positions, err := goExampleFiles(mod.Path, svc.RepoRoot)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		bounds := map[string]capturedBoundary{}
		for _, ex := range doc.Examples(files...) {
			if ex.Output == "" && !ex.EmptyOutput {
				continue
			}
			name := "Example" + ex.Name
			pos := positions[name]
			bounds[name] = normalizeBoundary(mod.ID, boundaryCapture{
				Name: name, File: pos.file, Line: pos.line,
				Cases: []caseCapture{{Name: "example", Output: strings.TrimSuffix(ex.Output, "\n")}},
			})
		}
		if len(bounds) > 0 {
			out[mod.ID] = bounds
		}
	}
	return out, nil
}

type examplePosition struct {
	file string
	line int
}

func goExampleFiles(moduleDir, repoRoot string) ([]*ast.File, map[string]examplePosition, error) {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	positions := map[string]examplePosition{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		abs := filepath.Join(moduleDir, entry.Name())
		file, err := parser.ParseFile(fset, abs, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("behavior: parse %s: %w", abs, err)
		}
		files = append(files, file)
		rel, _ := filepath.Rel(repoRoot, abs)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && strings.HasPrefix(fn.Name.Name, "Example") {
				positions[fn.Name.Name] = examplePosition{file: filepath.ToSlash(rel), line: fset.Position(fn.Pos()).Line}
			}
		}
	}
	return files, positions, nil
}

func goTestPatterns(svc plane.DeriveServices) []string {
	seen := map[string]bool{}
	var out []string
	for _, spec := range svc.Languages {
		if spec.Language != "go" {
			continue
		}
		for _, root := range spec.Roots {
			clean := filepath.ToSlash(filepath.Clean(root))
			pattern := "./..."
			if clean != "." && clean != "" {
				pattern = "./" + strings.TrimPrefix(clean, "./") + "/..."
			}
			if !seen[pattern] {
				seen[pattern] = true
				out = append(out, pattern)
			}
		}
	}
	if seen["./..."] || len(out) == 0 {
		return []string{"./..."}
	}
	sort.Strings(out)
	return out
}
