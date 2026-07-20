package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/plane"
)

func TestDeriveRealGoModule(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/dogfood\n\ngo 1.26\n")
	write(t, root, "domain/domain.go", "package domain\n\ntype Order struct{}\ntype Internal struct{}\n")
	write(t, root, "app/app.go", "package app\n\nimport \"example.test/dogfood/domain\"\n\nfunc Place() domain.Order { return domain.Order{} }\n")
	write(t, root, "app/app_test.go", "package app\n\nimport \"example.test/dogfood/domain\"\n\nvar _ = domain.Internal{}\n")

	owners := map[string]string{
		"domain/domain.go": "domain",
		"app/app.go":       "app",
	}
	svc := plane.DeriveServices{
		RepoRoot: root,
		Tools:    &derive.ExecRunner{RepoRoot: root},
		ModuleOf: func(file string) string { return owners[file] },
		FilesOf: func(id string) []string {
			if id == "domain" {
				return []string{"domain/domain.go"}
			}
			return []string{"app/app.go"}
		},
		UngovernedOf: func(string) string { return "" },
	}
	g, err := New().Derive(context.Background(), plane.LanguageSpec{
		Language: Language, Roots: []string{"."}, Tool: plane.ToolSpec{Name: "go"},
	}, svc, []string{"app", "domain"})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 || g.Edges[0].From != "app" || g.Edges[0].To != "domain" {
		t.Fatalf("edges = %#v", g.Edges)
	}
	if got := g.Module("domain").ReachableFromOutside; len(got) != 1 || got[0] != "Order" {
		t.Fatalf("reachable = %v (test-only Internal reference must be excluded)", got)
	}
	if len(g.Analyzers) != 2 || g.Analyzers[0].Language != "go" {
		t.Fatalf("analyzers = %#v", g.Analyzers)
	}
}

func TestGoVersion(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"go version go1.26.2 darwin/arm64", "1.26.2"},
		{"go1.25.1", "1.25.1"},
	} {
		got, err := goVersion(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("goVersion(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestGoListArgsIncludesDependenciesAndCollapsesRoot(t *testing.T) {
	got := goListArgs([]string{"internal", ".", "cmd"})
	want := []string{"list", "-deps", "-json", "./..."}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
