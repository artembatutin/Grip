package derive

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// TestNormalizeGolden checks the report→IR normalization against a hand-verified
// expectation: file-level imports collapse to module edges, entrypoint exports
// attach to modules, cross-module usage becomes reachable-from-outside, and
// reduced scopes become confidence records. This is the deriver golden test
// (plan/03 M0.3/M0.4) run offline against a recorded report.
func TestNormalizeGolden(t *testing.T) {
	rep := &AnalyzerReport{
		Tool:        AnalyzerInfo{Name: "dependency-cruiser", Version: "16"},
		SurfaceTool: AnalyzerInfo{Name: "ts-morph", Version: "22"},
		Exports: []ExportRec{
			{File: "src/domain/index.ts", Name: "Order", Kind: "class", Line: 2},
			{File: "src/app/index.ts", Name: "PlaceOrder", Kind: "class", Line: 3},
		},
		Imports: []ImportRec{
			{FromFile: "src/app/index.ts", ToFile: "src/domain/index.ts", Symbol: "Order", Line: 1, Kind: "import"},
			// intra-module reference: must NOT create an edge
			{FromFile: "src/app/index.ts", ToFile: "src/app/util.ts", Symbol: "helper", Line: 5, Kind: "import"},
			// external package: must NOT create an edge
			{FromFile: "src/app/index.ts", ToFile: "node_modules/x/index.js", Symbol: "x", Line: 6, External: true},
		},
		Reduced: []ReducedRec{{File: "src/legacy/loader.ts", Reason: "dynamic import()"}},
	}
	moduleOf := func(f string) string {
		switch {
		case has(f, "src/domain/"):
			return "src/domain"
		case has(f, "src/app/"):
			return "src/app"
		default:
			return "" // src/legacy is ungoverned
		}
	}
	filesOf := func(id string) []string { return nil }

	g, err := Normalize("typescript", rep, []string{"src/domain", "src/app"}, moduleOf, filesOf)
	if err != nil {
		t.Fatal(err)
	}

	if len(g.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (only the cross-module import)\n%+v", len(g.Edges), g.Edges)
	}
	e := g.Edges[0]
	if e.From != "src/app" || e.To != "src/domain" || e.Evidence[0].Symbol != "Order" || e.Evidence[0].Line != 1 {
		t.Fatalf("edge = %+v", e)
	}
	dom := g.Module("src/domain")
	if dom == nil || len(dom.ReachableFromOutside) != 1 || dom.ReachableFromOutside[0] != "Order" {
		t.Fatalf("domain reachable = %+v", dom)
	}
	if len(dom.Exports) != 1 || dom.Exports[0].Name != "Order" {
		t.Fatalf("domain exports = %+v", dom.Exports)
	}
	if len(g.Confidence) != 1 || g.Confidence[0].Level != "reduced" {
		t.Fatalf("confidence = %+v", g.Confidence)
	}
	if len(g.Analyzers) != 2 {
		t.Fatalf("analyzers = %+v", g.Analyzers)
	}
	// Determinism: normalizing again hashes identically.
	g2, _ := Normalize("typescript", rep, []string{"src/app", "src/domain"}, moduleOf, filesOf)
	if g.Hash() != g2.Hash() {
		t.Fatal("normalization not deterministic under reordered module ids")
	}
}

type reportRunner struct{ payload []byte }

func (r reportRunner) Run(context.Context, string, []string, []byte) ([]byte, error) {
	return r.payload, nil
}
func (r reportRunner) Version(context.Context, string) (string, error) { return "test", nil }

func TestRunHelperEnforcesConfiguredNameAndMinimum(t *testing.T) {
	rep := AnalyzerReport{Tool: AnalyzerInfo{Name: "dependency-cruiser", Version: "16.3.0"}, SurfaceTool: AnalyzerInfo{Name: "ts-morph", Version: "24.0.0"}}
	payload, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	svc := plane.DeriveServices{Tools: reportRunner{payload}, ModuleOf: func(string) string { return "" }, FilesOf: func(string) []string { return nil }}
	if _, err := RunHelper(context.Background(), "typescript", "typescript", plane.LanguageSpec{Tool: plane.ToolSpec{Name: "dependency-cruiser", MinVersion: "16.4.0"}}, svc, nil); err == nil {
		t.Fatal("old analyzer version passed")
	}
	if _, err := RunHelper(context.Background(), "typescript", "typescript", plane.LanguageSpec{Tool: plane.ToolSpec{Name: "deptrac"}}, svc, nil); err == nil {
		t.Fatal("different configured analyzer passed")
	}
}

func TestValidateReportRejectsUnknownConfidenceAndMalformedEvidence(t *testing.T) {
	base := AnalyzerReport{Tool: AnalyzerInfo{Name: "dependency-cruiser", Version: "16.3.0"}, SurfaceTool: AnalyzerInfo{Name: "ts-morph", Version: "24.0.0"}}
	base.Reduced = []ReducedRec{{File: "src/a.ts", Reason: "dynamic", Level: "maybe"}}
	if err := ValidateReport("typescript", plane.ToolSpec{Name: "dependency-cruiser"}, &base); err == nil {
		t.Fatal("unknown confidence passed")
	}
	base.Reduced = nil
	base.Imports = []ImportRec{{FromFile: "src/a.ts", ToFile: "src/b.ts", Symbol: "B", Line: 0, Kind: "import"}}
	if err := ValidateReport("typescript", plane.ToolSpec{Name: "dependency-cruiser"}, &base); err == nil {
		t.Fatal("line-zero evidence passed")
	}
}

func TestPackageOnlyImportCreatesEdgeWithoutFacadeReachability(t *testing.T) {
	rep := &AnalyzerReport{
		Tool:        AnalyzerInfo{Name: "go", Version: "1.26.2"},
		SurfaceTool: AnalyzerInfo{Name: "go/ast", Version: "1.26.2"},
		Imports: []ImportRec{{
			FromFile: "cmd/app/main.go", ToFile: "internal/plugin/plugin.go", Line: 3,
			Kind: "import", PackageOnly: true,
		}},
	}
	moduleOf := func(file string) string {
		if has(file, "cmd/app/") {
			return "cmd/app"
		}
		if has(file, "internal/plugin/") {
			return "internal/plugin"
		}
		return ""
	}
	g, err := Normalize("go", rep, []string{"cmd/app", "internal/plugin"}, moduleOf, func(string) []string { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("edges = %#v", g.Edges)
	}
	if got := g.Module("internal/plugin").ReachableFromOutside; len(got) != 0 {
		t.Fatalf("package-only import widened facade: %v", got)
	}
}

func has(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
