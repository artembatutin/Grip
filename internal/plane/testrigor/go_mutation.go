package testrigor

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
)

const maxGoMutantsPerModule = 24

type goMarkedTest struct {
	id        string
	behaviors []string
	contract  bool
	skipped   bool
	file      string
	line      int
}

type goMutation struct {
	file        string
	offset      int
	length      int
	replacement string
}

// deriveGoTestRigor executes marked Go boundary tests and then runs deterministic
// source mutants through Go's overlay mechanism. The working tree is never
// modified: each mutant exists only in a temporary replacement file.
func deriveGoTestRigor(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) ([]*ModuleState, error) {
	refs := append([]plane.ModuleRef(nil), mods...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	states := make([]*ModuleState, 0, len(refs))
	for _, mod := range refs {
		marked, err := markedGoTests(mod, svc.RepoRoot)
		if err != nil {
			return nil, err
		}
		st := &ModuleState{ModuleID: mod.ID, Language: mod.Language, Analyzed: len(marked) > 0}
		if len(marked) == 0 {
			states = append(states, st)
			continue
		}
		pkg := "./" + filepath.ToSlash(mod.ID)
		if _, err := svc.Tools.Run(ctx, "go", []string{"test", "-count=1", pkg}, nil); err != nil {
			return nil, fmt.Errorf("test-rigor: Go boundary tests failed for %s: %w", mod.ID, err)
		}
		for _, mt := range marked {
			st.Tests = append(st.Tests, TestState{
				ID: mt.id, Behaviors: append([]string(nil), mt.behaviors...), Contract: mt.contract,
				Skipped: mt.skipped, File: mt.file, Line: mt.line,
			})
			if mt.contract {
				st.ContractPresent = true
				if st.ContractTestID == "" {
					st.ContractTestID, st.ContractFile, st.ContractLine = mt.id, mt.file, mt.line
				}
			}
		}

		mutants, err := goMutations(svc.RepoRoot, svc.FilesOf(mod.ID))
		if err != nil {
			return nil, err
		}
		if len(mutants) > maxGoMutantsPerModule {
			mutants = mutants[:maxGoMutantsPerModule]
		}
		killed := 0
		for _, mutant := range mutants {
			dead, err := runGoMutant(ctx, svc, pkg, mutant)
			if err != nil {
				return nil, err
			}
			if dead {
				killed++
			}
		}
		st.ContractMutants = len(mutants)
		st.ContractKilled = killed
		st.MutationScore = pct(killed, len(mutants))
		states = append(states, st)
	}
	return states, nil
}

func markedGoTests(mod plane.ModuleRef, repoRoot string) ([]goMarkedTest, error) {
	entries, err := os.ReadDir(mod.Path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []goMarkedTest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		abs := filepath.Join(mod.Path, entry.Name())
		file, err := parser.ParseFile(fset, abs, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("test-rigor: parse %s: %w", abs, err)
		}
		rel, _ := filepath.Rel(repoRoot, abs)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			marker := parseGoTestMarker(fn.Doc.Text())
			if marker == nil {
				continue
			}
			marker.id = fn.Name.Name
			marker.file = filepath.ToSlash(rel)
			marker.line = fset.Position(fn.Pos()).Line
			marker.skipped = containsSkip(fn.Body)
			out = append(out, *marker)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func parseGoTestMarker(docText string) *goMarkedTest {
	for _, line := range strings.Split(docText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "grip:test") {
			continue
		}
		mt := &goMarkedTest{}
		for _, field := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "grip:test"))) {
			switch {
			case field == "contract":
				mt.contract = true
			case strings.HasPrefix(field, "behavior="):
				for _, behavior := range strings.Split(strings.TrimPrefix(field, "behavior="), ",") {
					if behavior != "" {
						mt.behaviors = append(mt.behaviors, behavior)
					}
				}
			}
		}
		return mt
	}
	return nil
}

func containsSkip(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (sel.Sel.Name == "Skip" || sel.Sel.Name == "SkipNow" || sel.Sel.Name == "Skipf") {
			found = true
			return false
		}
		return true
	})
	return found
}

func goMutations(repoRoot string, files []string) ([]goMutation, error) {
	var out []goMutation
	for _, rel := range files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, abs, nil, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BinaryExpr:
				if replacement, ok := mutatedOperator(n.Op); ok {
					pos := fset.Position(n.OpPos)
					out = append(out, goMutation{file: abs, offset: pos.Offset, length: len(n.Op.String()), replacement: replacement})
				}
			case *ast.Ident:
				if n.Name == "true" || n.Name == "false" {
					pos := fset.Position(n.Pos())
					replacement := "true"
					if n.Name == "true" {
						replacement = "false"
					}
					out = append(out, goMutation{file: abs, offset: pos.Offset, length: len(n.Name), replacement: replacement})
				}
			}
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].offset < out[j].offset
	})
	return out, nil
}

func mutatedOperator(op token.Token) (string, bool) {
	switch op {
	case token.EQL:
		return "!=", true
	case token.NEQ:
		return "==", true
	case token.LSS:
		return ">=", true
	case token.LEQ:
		return ">", true
	case token.GTR:
		return "<=", true
	case token.GEQ:
		return "<", true
	case token.ADD:
		return "-", true
	case token.SUB:
		return "+", true
	default:
		return "", false
	}
}

func runGoMutant(ctx context.Context, svc plane.DeriveServices, pkg string, mutant goMutation) (bool, error) {
	original, err := os.ReadFile(mutant.file)
	if err != nil {
		return false, err
	}
	if mutant.offset < 0 || mutant.offset+mutant.length > len(original) {
		return false, fmt.Errorf("test-rigor: invalid mutation offset for %s", mutant.file)
	}
	body := make([]byte, 0, len(original)-mutant.length+len(mutant.replacement))
	body = append(body, original[:mutant.offset]...)
	body = append(body, mutant.replacement...)
	body = append(body, original[mutant.offset+mutant.length:]...)
	dir, err := os.MkdirTemp("", "grip-go-mutant-*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	mutantFile := filepath.Join(dir, filepath.Base(mutant.file))
	if err := os.WriteFile(mutantFile, body, 0o600); err != nil {
		return false, err
	}
	overlayFile := filepath.Join(dir, "overlay.json")
	overlay, _ := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{mutant.file: mutantFile}})
	if err := os.WriteFile(overlayFile, overlay, 0o600); err != nil {
		return false, err
	}
	_, runErr := svc.Tools.Run(ctx, "go", []string{"test", "-count=1", "-overlay", overlayFile, pkg}, nil)
	return runErr != nil, nil
}
