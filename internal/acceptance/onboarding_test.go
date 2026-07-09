package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/manifest"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/ratify"
)

// TestOnboardingGenerateThenRatify proves M0.10 / GR-X-4: a repo with ZERO
// module manifests goes to a green gate in one sitting. We strip every grip.yaml
// (and the reduced-confidence legacy corner, which is intentionally left for the
// human), run the generate step over candidate modules, write the drafts, and
// assert the gate now passes.
func TestOnboardingGenerateThenRatify(t *testing.T) {
	fx := fixturesDir(t)
	root := t.TempDir()
	copyTree(t, filepath.Join(fx, "base"), root)

	// Remove all module manifests (keep .grip.yaml) and the ungoverned dynamic
	// corner, simulating a brownfield repo before onboarding.
	removeModuleManifests(t, root)
	_ = os.RemoveAll(filepath.Join(root, "src", "legacy"))
	stripLegacyReduced(t, root)

	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	tools := &derive.RecordedRunner{AnalysisDir: filepath.Join(root, ".grip-analysis")}

	// Sanity: before onboarding there are no governed modules.
	pre, err := gate.Run(context.Background(), cfg, reg, gate.Options{CI: true, Tools: tools, Commit: "pre"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre.Governed) != 0 {
		t.Fatalf("expected no governed modules before init, got %v", pre.Governed)
	}

	// Generate step: derive from candidate modules and draft manifests.
	cand, err := manifest.Candidates(root, cfg.LanguageRoots())
	if err != nil {
		t.Fatal(err)
	}
	svc := plane.DeriveServices{
		Commit: "init", RepoRoot: root, Tools: tools,
		ModuleOf: cand.ModuleOf, FilesOf: cand.FilesOf, Languages: cfg.LanguageSpecs(),
	}
	g, err := cli.BuildOrchestrator().Derive(context.Background(), cand.Refs(), svc)
	if err != nil {
		t.Fatal(err)
	}
	drafts := ratify.DraftManifests(g)
	if len(drafts) == 0 {
		t.Fatal("expected generated draft manifests")
	}
	for _, f := range drafts {
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Ratify: the gate must now be green.
	post, err := gate.Run(context.Background(), cfg, reg, gate.Options{CI: true, Tools: tools, Commit: "post"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Decision != "pass" {
		t.Fatalf("onboarding did not reach green: %s\n%s", post.Decision, renderHuman(post))
	}
	if len(post.Governed) == 0 {
		t.Fatal("expected governed modules after init")
	}
}

func removeModuleManifests(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == manifest.Filename && filepath.Dir(p) != root {
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stripLegacyReduced rewrites the recorded TS report to drop the src/legacy
// reduced-confidence record, since we removed that directory for the onboarding
// baseline.
func stripLegacyReduced(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, ".grip-analysis", "typescript.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var rep map[string]interface{}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	rep["reduced"] = []interface{}{}
	out, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
}
