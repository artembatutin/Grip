// Package config loads and validates the repo-root .grip.yaml (plan/01 §6,
// plan/03 M0.1). It selects enabled planes, declares language roots and their
// analyzer tools, and carries tier promotions and layer order. Every malformed
// or ambiguous condition is fail-closed: unknown plane, unsupported language,
// unknown promotion target, or a promote naming a non-existent rule all block.
//
// This is one of only two engine packages permitted to know plane ids by name
// (the other is the registry), because it maps the config's plane keys onto
// registered planes. It still hard-codes none: plane names come from YAML and
// are validated against the registry.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/artembatutin/grip/internal/manifest"
	"github.com/artembatutin/grip/internal/plane"
	"gopkg.in/yaml.v3"
)

// Filename is the fixed repo-root config file name.
const Filename = ".grip.yaml"

// Config is the validated repo configuration.
type Config struct {
	Version   int                       `yaml:"version"`
	Planes    map[string]PlaneConfig    `yaml:"planes"`
	Languages map[string]LanguageConfig `yaml:"languages"`
	Modules   ModulesConfig             `yaml:"modules"`
	Policy    PolicyConfig              `yaml:"policy"`
	Gate      GateConfig                `yaml:"gate"`

	Path     string `yaml:"-"`
	RepoRoot string `yaml:"-"`
}

// PlaneConfig enables/disables a plane.
type PlaneConfig struct {
	Enabled bool `yaml:"enabled"`
}

// LanguageConfig declares where a language's source lives and its analyzer.
type LanguageConfig struct {
	Roots []string   `yaml:"roots"`
	Tool  ToolConfig `yaml:"tool"`
}

// ToolConfig names the analyzer binary and an optional minimum version.
type ToolConfig struct {
	Name       string `yaml:"name"`
	MinVersion string `yaml:"minVersion"`
}

// ModulesConfig controls module granularity (directory-based in M0, D4).
type ModulesConfig struct {
	Granularity string `yaml:"granularity"`
}

// PolicyConfig carries tier promotions and layer order.
type PolicyConfig struct {
	Promote []PromoteRule `yaml:"promote"`
	Layers  LayersConfig  `yaml:"layers"`
}

// PromoteRule promotes a Tier B rule to Tier A ("block") for this repo.
type PromoteRule struct {
	Rule string `yaml:"rule"`
	To   string `yaml:"to"`
}

// LayersConfig declares an architectural layer order for direction rules.
type LayersConfig struct {
	Order []string `yaml:"order"`
}

// GateConfig configures gate behavior. FailClosed is always true (auditability
// only — it cannot be disabled).
type GateConfig struct {
	FailClosed bool           `yaml:"failClosed"`
	Local      GateModeConfig `yaml:"local"`
	CI         GateModeConfig `yaml:"ci"`
}

// GateModeConfig selects the plane subset run in a given mode.
type GateModeConfig struct {
	Planes []string `yaml:"planes"`
}

// supportedLanguages maps a config language name to the source extensions
// discovery treats as that language's code. M0 supports PHP and TS/JS (D2).
var supportedLanguages = map[string][]string{
	"typescript": {".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".cts", ".mts"},
	"php":        {".php"},
}

var supportedAnalyzers = map[string]map[string]bool{
	"typescript": {"dependency-cruiser": true, "stryker": true},
	"php":        {"deptrac": true, "infection": true},
}

// Load reads, parses (strictly), defaults, and validates .grip.yaml against the
// registry. A missing file, malformed YAML, or any validation failure is a
// fail-closed error (exit 3 at the CLI boundary for config/usage errors).
func Load(repoRoot string, reg *plane.Registry) (*Config, error) {
	path := filepath.Join(repoRoot, Filename)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Filename, err)
	}
	cfg, err := parse(raw)
	if err != nil {
		return nil, err
	}
	cfg.Path = path
	cfg.RepoRoot = repoRoot
	cfg.applyDefaults()
	if err := cfg.validate(reg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parse(raw []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Filename, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Modules.Granularity == "" {
		c.Modules.Granularity = "directory"
	}
	// Fail-closed cannot be disabled; force it on regardless of the file so the
	// value is honest for auditing (principle 6).
	c.Gate.FailClosed = true
}

func (c *Config) validate(reg *plane.Registry) error {
	if c.Version != 1 {
		return fmt.Errorf("%s: unsupported version %d (engine speaks version 1)", Filename, c.Version)
	}
	// Every configured plane must be a registered plane (fail-closed on unknown).
	for name := range c.Planes {
		if _, ok := reg.Get(name); !ok {
			return fmt.Errorf("%s: unknown plane %q (enable only registered planes)", Filename, name)
		}
	}
	if len(c.EnabledPlanes()) == 0 {
		return fmt.Errorf("%s: no planes enabled; nothing to govern", Filename)
	}
	// Every configured language must be supported and declare at least one root.
	for name, lc := range c.Languages {
		if _, ok := supportedLanguages[name]; !ok {
			return fmt.Errorf("%s: unsupported language %q (M0 supports php and typescript)", Filename, name)
		}
		if len(lc.Roots) == 0 {
			return fmt.Errorf("%s: language %q declares no roots", Filename, name)
		}
		if !supportedAnalyzers[name][lc.Tool.Name] {
			return fmt.Errorf("%s: language %q does not support analyzer %q", Filename, name, lc.Tool.Name)
		}
	}
	if c.Modules.Granularity != "directory" {
		return fmt.Errorf("%s: modules.granularity %q unsupported (M0 supports 'directory')", Filename, c.Modules.Granularity)
	}
	// Promotions must name a real, promotable rule and target "block".
	known := knownRules(reg)
	for _, pr := range c.Policy.Promote {
		spec, ok := known[pr.Rule]
		if !ok {
			return fmt.Errorf("%s: policy.promote names unknown rule %q", Filename, pr.Rule)
		}
		// Tier C is judgment-assisted (LLM) and must never gate a merge (principle
		// 3). Refuse to promote it here regardless of the rule's Promotable flag, so
		// a plane that mistakenly marks a Tier C rule promotable still cannot make an
		// LLM signal blocking. The gate excludes Tier C structurally too; this is the
		// authoring-time guard that reports the mistake clearly.
		if spec.Tier == plane.TierC {
			return fmt.Errorf("%s: rule %q is Tier C (judgment-assisted) and cannot be promoted to blocking", Filename, pr.Rule)
		}
		if !spec.Promotable {
			return fmt.Errorf("%s: rule %q is not promotable", Filename, pr.Rule)
		}
		if pr.To != "block" {
			return fmt.Errorf("%s: policy.promote for %q must target 'block', got %q", Filename, pr.Rule, pr.To)
		}
	}
	// Gate mode plane subsets must reference enabled planes only.
	enabled := map[string]bool{}
	for _, p := range c.EnabledPlanes() {
		enabled[p] = true
	}
	for _, mode := range []struct {
		name   string
		planes []string
	}{{"local", c.Gate.Local.Planes}, {"ci", c.Gate.CI.Planes}} {
		for _, p := range mode.planes {
			if !enabled[p] {
				return fmt.Errorf("%s: gate.%s references plane %q that is not enabled", Filename, mode.name, p)
			}
		}
	}
	return nil
}

// EnabledPlanes returns the sorted ids of planes enabled in config.
func (c *Config) EnabledPlanes() []string {
	var out []string
	for name, pc := range c.Planes {
		if pc.Enabled {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// PlanesForMode returns the plane ids to run in the given gate mode. If the mode
// declares no explicit subset, all enabled planes run.
func (c *Config) PlanesForMode(ci bool) []string {
	var subset []string
	if ci {
		subset = c.Gate.CI.Planes
	} else {
		subset = c.Gate.Local.Planes
	}
	if len(subset) == 0 {
		return c.EnabledPlanes()
	}
	out := append([]string(nil), subset...)
	sort.Strings(out)
	return out
}

// LanguageRoots converts the config's languages into discovery inputs, attaching
// the known extension set for each language.
func (c *Config) LanguageRoots() []manifest.LanguageRoots {
	names := make([]string, 0, len(c.Languages))
	for name := range c.Languages {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]manifest.LanguageRoots, 0, len(names))
	for _, name := range names {
		out = append(out, manifest.LanguageRoots{
			Language: name,
			Roots:    c.Languages[name].Roots,
			Exts:     supportedLanguages[name],
		})
	}
	return out
}

// LanguageSpecs converts the config's languages into plane.LanguageSpec values
// (roots + tool) for the deriver, in stable language order.
func (c *Config) LanguageSpecs() []plane.LanguageSpec {
	var out []plane.LanguageSpec
	for name, lc := range c.Languages {
		out = append(out, plane.LanguageSpec{
			Language: name,
			Roots:    lc.Roots,
			Tool:     plane.ToolSpec{Name: lc.Tool.Name, MinVersion: lc.Tool.MinVersion},
		})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Language < out[b].Language })
	return out
}

// PromotedRules returns the set of rule ids promoted to Tier A for this repo.
func (c *Config) PromotedRules() map[string]bool {
	out := map[string]bool{}
	for _, pr := range c.Policy.Promote {
		if pr.To == "block" {
			out[pr.Rule] = true
		}
	}
	return out
}

func knownRules(reg *plane.Registry) map[string]plane.RuleSpec {
	out := map[string]plane.RuleSpec{}
	for _, p := range reg.All() {
		for _, r := range p.Rules() {
			out[r.ID] = r
		}
	}
	return out
}
