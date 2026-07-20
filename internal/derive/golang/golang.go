// Package golang derives Go package dependencies and exported package symbols
// into Grip's language-neutral AnalyzerReport and Common Graph IR.
//
// The Go tool owns package/build-context resolution (`go list`); the standard
// parser supplies stable file:line:symbol evidence. Test files are deliberately
// excluded so test harness dependencies do not become production architecture.
package golang

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Language is the config/IR language key this deriver owns.
const Language = "go"

type deriver struct{}

// New returns the Go architecture deriver.
func New() derive.Deriver { return deriver{} }

func (deriver) Language() string { return Language }

// goPackage is the stable subset of `go list -json` used by the deriver.
type goPackage struct {
	Dir        string
	ImportPath string
	Name       string
	GoFiles    []string
	CgoFiles   []string
}

func (deriver) Derive(ctx context.Context, spec plane.LanguageSpec, svc plane.DeriveServices, moduleIDs []string) (*ir.Graph, error) {
	if spec.Tool.Name != "go" {
		return nil, fmt.Errorf("go deriver: configured analyzer %q, want %q", spec.Tool.Name, "go")
	}
	versionRaw, err := svc.Tools.Version(ctx, "go")
	if err != nil {
		return nil, err
	}
	version, err := goVersion(versionRaw)
	if err != nil {
		return nil, fmt.Errorf("go deriver: %w", err)
	}

	out, err := svc.Tools.Run(ctx, "go", goListArgs(spec.Roots), nil)
	if err != nil {
		return nil, err
	}
	pkgs, err := decodePackages(out)
	if err != nil {
		return nil, fmt.Errorf("go deriver: parse go list output: %w", err)
	}
	rep, err := analyze(svc.RepoRoot, pkgs)
	if err != nil {
		return nil, err
	}
	rep.Tool = derive.AnalyzerInfo{Name: "go", Version: version}
	rep.SurfaceTool = derive.AnalyzerInfo{Name: "go/ast", Version: parserVersion()}
	if err := derive.ValidateReport(Language, spec.Tool, rep); err != nil {
		return nil, err
	}
	return derive.Normalize(Language, rep, moduleIDs, svc.ModuleOf, svc.FilesOf, svc.UngovernedOf)
}

func goListArgs(roots []string) []string {
	patterns := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		clean := filepath.ToSlash(filepath.Clean(root))
		pattern := "./..."
		if clean != "." && clean != "" {
			pattern = "./" + strings.TrimPrefix(clean, "./") + "/..."
		}
		if !seen[pattern] {
			seen[pattern] = true
			patterns = append(patterns, pattern)
		}
	}
	// The repository root subsumes every narrower package pattern.
	if seen["./..."] {
		patterns = []string{"./..."}
	}
	if len(patterns) == 0 {
		patterns = append(patterns, "./...")
	}
	sort.Strings(patterns)
	// -deps is load-bearing: without it, a local import outside the configured
	// roots would be absent from the package index and could look external.
	return append([]string{"list", "-deps", "-json"}, patterns...)
}

func decodePackages(raw []byte) ([]goPackage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	byImport := map[string]goPackage{}
	for {
		var pkg goPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if pkg.Dir == "" || pkg.ImportPath == "" || pkg.Name == "" {
			return nil, fmt.Errorf("package is missing Dir, ImportPath, or Name")
		}
		byImport[pkg.ImportPath] = pkg
	}
	out := make([]goPackage, 0, len(byImport))
	for _, pkg := range byImport {
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImportPath < out[j].ImportPath })
	return out, nil
}

func analyze(repoRoot string, pkgs []goPackage) (*derive.AnalyzerReport, error) {
	rep := &derive.AnalyzerReport{}
	byImport := make(map[string]goPackage, len(pkgs))
	for _, pkg := range pkgs {
		if withinRepo(repoRoot, pkg.Dir) {
			byImport[pkg.ImportPath] = pkg
		}
	}

	for _, pkg := range pkgs {
		if !withinRepo(repoRoot, pkg.Dir) {
			continue
		}
		files := append(append([]string(nil), pkg.GoFiles...), pkg.CgoFiles...)
		sort.Strings(files)
		for _, name := range files {
			abs := filepath.Join(pkg.Dir, name)
			rel, err := repoRelative(repoRoot, abs)
			if err != nil {
				return nil, err
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, abs, nil, parser.ParseComments)
			if err != nil {
				return nil, fmt.Errorf("go deriver: parse %s: %w", rel, err)
			}
			appendExports(rep, fset, file, rel)
			if err := appendImports(rep, fset, file, rel, repoRoot, byImport); err != nil {
				return nil, err
			}
		}
	}
	return rep, nil
}

type localImport struct {
	target derive.ImportRec
	used   bool
}

func appendImports(rep *derive.AnalyzerReport, fset *token.FileSet, file *ast.File, rel, repoRoot string, byImport map[string]goPackage) error {
	aliases := map[string]*localImport{}
	for _, imp := range file.Imports {
		pathValue, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return fmt.Errorf("go deriver: parse import in %s: %w", rel, err)
		}
		targetPkg, local := byImport[pathValue]
		if !local {
			continue
		}
		toFile, err := representativeFile(repoRoot, targetPkg)
		if err != nil {
			return err
		}
		line := fset.Position(imp.Pos()).Line
		base := derive.ImportRec{FromFile: rel, ToFile: toFile, Line: line, Kind: "import"}
		alias := targetPkg.Name
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		switch alias {
		case "_":
			base.PackageOnly = true
			rep.Imports = append(rep.Imports, base)
		case ".":
			base.PackageOnly = true
			rep.Imports = append(rep.Imports, base)
			rep.Reduced = append(rep.Reduced, derive.ReducedRec{
				File: rel, Reason: "dot import prevents reliable package-symbol attribution", Level: string(ir.LevelReduced),
			})
		default:
			aliases[alias] = &localImport{target: base}
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		local := aliases[ident.Name]
		if local == nil {
			return true
		}
		rec := local.target
		rec.Symbol = sel.Sel.Name
		rec.Line = fset.Position(sel.Sel.Pos()).Line
		rep.Imports = append(rep.Imports, rec)
		local.used = true
		return true
	})
	for _, alias := range sortedAliases(aliases) {
		local := aliases[alias]
		if !local.used {
			// Valid Go normally cannot have an unused named import. Keeping the
			// dependency still makes partially generated source fail closed rather
			// than silently dropping a package edge.
			rec := local.target
			rec.PackageOnly = true
			rep.Imports = append(rep.Imports, rec)
		}
	}
	return nil
}

func appendExports(rep *derive.AnalyzerReport, fset *token.FileSet, file *ast.File, rel string) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && ast.IsExported(d.Name.Name) {
				rep.Exports = append(rep.Exports, export(rel, d.Name.Name, "function", fset.Position(d.Name.Pos()).Line))
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(s.Name.Name) {
						rep.Exports = append(rep.Exports, export(rel, s.Name.Name, "type", fset.Position(s.Name.Pos()).Line))
					}
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						if ast.IsExported(name.Name) {
							rep.Exports = append(rep.Exports, export(rel, name.Name, kind, fset.Position(name.Pos()).Line))
						}
					}
				}
			}
		}
	}
}

func export(file, name, kind string, line int) derive.ExportRec {
	return derive.ExportRec{File: file, Name: name, Kind: kind, Line: line}
}

func representativeFile(repoRoot string, pkg goPackage) (string, error) {
	files := append(append([]string(nil), pkg.GoFiles...), pkg.CgoFiles...)
	if len(files) == 0 {
		return "", fmt.Errorf("go deriver: local package %s has no production Go files", pkg.ImportPath)
	}
	sort.Strings(files)
	return repoRelative(repoRoot, filepath.Join(pkg.Dir, files[0]))
}

func repoRelative(repoRoot, path string) (string, error) {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("go deriver: path %q is outside repository %q", path, repoRoot)
	}
	return filepath.ToSlash(rel), nil
}

func withinRepo(repoRoot, dir string) bool {
	_, err := repoRelative(repoRoot, dir)
	return err == nil
}

func sortedAliases(m map[string]*localImport) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func goVersion(raw string) (string, error) {
	for _, field := range strings.Fields(raw) {
		if strings.HasPrefix(field, "go1.") {
			return strings.TrimPrefix(field, "go"), nil
		}
	}
	return "", fmt.Errorf("cannot resolve semantic Go version from %q", strings.TrimSpace(raw))
}

func parserVersion() string {
	version, err := goVersion(runtime.Version())
	if err != nil {
		// Development toolchains can report devel identifiers. The dependency
		// analyzer remains versioned; use a valid conservative surface version.
		return "0.0.0"
	}
	return version
}
