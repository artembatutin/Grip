package testrigor

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
	"gopkg.in/yaml.v3"
)

// goTestRigorBaseline derives the previous required-test inventory and threshold
// directly from Git HEAD. A missing Git history is benign on first adoption;
// once committed, deleting a marked test or lowering the manifest threshold is
// compared against evidence the working-tree change cannot rewrite.
func goTestRigorBaseline(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) map[string]*BaselineState {
	out := map[string]*BaselineState{}
	for _, mod := range mods {
		if mod.Language != "go" {
			continue
		}
		manifestRaw, ok := gitShow(ctx, svc.RepoRoot, filepath.ToSlash(filepath.Join(mod.ID, "grip.yaml")))
		if !ok {
			continue
		}
		var envelope struct {
			TestRigor struct {
				MutationThreshold *int `yaml:"mutationThreshold"`
			} `yaml:"testRigor"`
		}
		if yaml.Unmarshal(manifestRaw, &envelope) != nil {
			continue
		}
		bs := &BaselineState{RequiredTests: map[string][]string{}}
		if envelope.TestRigor.MutationThreshold != nil {
			bs.Threshold = clampPct(*envelope.TestRigor.MutationThreshold)
			bs.HasThreshold = true
		}
		files := gitFiles(ctx, svc.RepoRoot, mod.ID)
		for _, rel := range files {
			if !strings.HasSuffix(rel, "_test.go") {
				continue
			}
			raw, ok := gitShow(ctx, svc.RepoRoot, rel)
			if !ok {
				continue
			}
			for _, mt := range markedTestsFromSource(raw) {
				for _, behavior := range mt.behaviors {
					bs.RequiredTests[behavior] = append(bs.RequiredTests[behavior], mt.id)
				}
			}
		}
		for behavior := range bs.RequiredTests {
			sort.Strings(bs.RequiredTests[behavior])
		}
		if bs.HasThreshold || len(bs.RequiredTests) > 0 {
			out[mod.ID] = bs
		}
	}
	return out
}

func gitShow(ctx context.Context, repoRoot, rel string) ([]byte, bool) {
	cmd := exec.CommandContext(ctx, "git", "show", "HEAD:"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	return out, err == nil
}

func gitFiles(ctx context.Context, repoRoot, moduleID string) []string {
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", "HEAD", "--", moduleID)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	sort.Strings(files)
	return files
}

func markedTestsFromSource(raw []byte) []goMarkedTest {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "baseline_test.go", bytes.NewReader(raw), parser.ParseComments)
	if err != nil {
		return nil
	}
	var out []goMarkedTest
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
		out = append(out, *marker)
	}
	return out
}
