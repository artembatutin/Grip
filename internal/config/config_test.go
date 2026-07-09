package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/plane/architecture"
)

func testRegistry() *plane.Registry {
	reg := plane.NewRegistry()
	reg.Register(architecture.New(nil)) // Rules() needs no deriver
	return reg
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const validConfig = `version: 1
planes:
  architecture: { enabled: true }
languages:
  typescript:
    roots: ["src"]
    tool: { name: dependency-cruiser }
modules:
  granularity: directory
gate:
  local: { planes: [architecture] }
  ci: { planes: [architecture] }
`

func TestLoadValid(t *testing.T) {
	dir := writeConfig(t, validConfig)
	cfg, err := Load(dir, testRegistry())
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if got := cfg.EnabledPlanes(); len(got) != 1 || got[0] != "architecture" {
		t.Fatalf("enabled planes = %v", got)
	}
	if !cfg.Gate.FailClosed {
		t.Fatal("failClosed must be forced true")
	}
}

func TestFailClosedConditions(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"unknown version", "version: 2\nplanes:\n  architecture: { enabled: true }\n"},
		{"unknown plane", "version: 1\nplanes:\n  telepathy: { enabled: true }\n"},
		{"no planes enabled", "version: 1\nplanes:\n  architecture: { enabled: false }\n"},
		{"unsupported language", "version: 1\nplanes:\n  architecture: { enabled: true }\nlanguages:\n  cobol:\n    roots: [\"x\"]\n"},
		{"unknown top-level key", validConfig + "bogus: true\n"},
		{"bad granularity", "version: 1\nplanes:\n  architecture: { enabled: true }\nmodules:\n  granularity: package\n"},
		{"promote unknown rule", "version: 1\nplanes:\n  architecture: { enabled: true }\npolicy:\n  promote:\n    - rule: arch.nonsense\n      to: block\n"},
		{"promote tier C rule", "version: 1\nplanes:\n  architecture: { enabled: true }\npolicy:\n  promote:\n    - rule: arch.unclear-name\n      to: block\n"},
		{"gate references disabled plane", "version: 1\nplanes:\n  architecture: { enabled: true }\ngate:\n  ci: { planes: [behavior] }\n"},
		{"malformed yaml", "version: 1\nplanes: [this, is, wrong]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeConfig(t, tc.body)
			if _, err := Load(dir, testRegistry()); err == nil {
				t.Fatalf("expected fail-closed error, got nil")
			}
		})
	}
}

func TestPromoteValidatesPromotableRule(t *testing.T) {
	body := "version: 1\nplanes:\n  architecture: { enabled: true }\npolicy:\n  promote:\n    - rule: arch.duplication\n      to: block\n"
	dir := writeConfig(t, body)
	cfg, err := Load(dir, testRegistry())
	if err != nil {
		t.Fatalf("promoting a promotable rule should pass: %v", err)
	}
	if !cfg.PromotedRules()["arch.duplication"] {
		t.Fatal("expected arch.duplication promoted")
	}
}

func TestMissingConfigIsError(t *testing.T) {
	if _, err := Load(t.TempDir(), testRegistry()); err == nil {
		t.Fatal("missing .grip.yaml must be an error")
	}
}

// fakePlane is a minimal plane whose only interesting behavior is Rules(), used
// to exercise config validation against arbitrary rule specs.
type fakePlane struct {
	id    string
	rules []plane.RuleSpec
}

func (f *fakePlane) ID() string              { return f.id }
func (f *fakePlane) ManifestSection() string { return f.id }
func (f *fakePlane) ParseIntent(plane.ManifestSection, plane.ModuleRef) (plane.Intent, error) {
	return nil, nil
}
func (f *fakePlane) Derive(context.Context, []plane.ModuleRef, plane.DeriveServices) (plane.Derived, error) {
	return nil, nil
}
func (f *fakePlane) Reconcile(map[string]plane.Intent, plane.Derived) []plane.Violation { return nil }
func (f *fakePlane) Rules() []plane.RuleSpec                                            { return f.rules }

// TestTierCCannotBePromotedEvenIfMarkedPromotable proves the promotion guard keys
// on the tier, not only on the Promotable flag: a plane that mistakenly declares a
// Tier C rule as Promotable still cannot make that judgment-assisted (LLM) signal
// blocking. This is the defence-in-depth behind gate.decide's structural Tier C
// exclusion — the config catches the mistake at authoring time with a clear error.
func TestTierCCannotBePromotedEvenIfMarkedPromotable(t *testing.T) {
	reg := plane.NewRegistry()
	reg.Register(&fakePlane{id: "fake", rules: []plane.RuleSpec{
		{ID: "fake.judgment", Tier: plane.TierC, Promotable: true}, // a mistaken declaration
	}})
	body := "version: 1\nplanes:\n  fake: { enabled: true }\npolicy:\n  promote:\n    - rule: fake.judgment\n      to: block\n"
	dir := writeConfig(t, body)
	_, err := Load(dir, reg)
	if err == nil {
		t.Fatal("promoting a Tier C rule must be rejected even when Promotable is true")
	}
	if !strings.Contains(err.Error(), "Tier C") {
		t.Fatalf("error should explain that Tier C cannot be promoted, got: %v", err)
	}
}

// TestNoRegisteredRuleIsPromotableTierC is a contract invariant across every plane
// this test package can see: a rule may not be both Tier C and Promotable. It
// guards future plane authors from a declaration that the promotion guard would
// then have to reject at runtime.
func TestNoRegisteredRuleIsPromotableTierC(t *testing.T) {
	for _, p := range testRegistry().All() {
		for _, r := range p.Rules() {
			if r.Tier == plane.TierC && r.Promotable {
				t.Errorf("plane %q rule %q is Tier C and Promotable — Tier C rules must never be promotable", p.ID(), r.ID)
			}
		}
	}
}
