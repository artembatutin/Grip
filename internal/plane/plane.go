// Package plane defines the plugin contract that keeps the engine plane-agnostic
// (plan/02). A plane governs exactly one axis of drift and contributes three
// things and nothing else: a manifest schema, a deriver, and a tiered rule set.
// The engine owns discovery, config, orchestration, the gate decision, diff,
// reporting, exit codes, and fail-closed policy.
//
// The contract exists in M0 even though only the Architecture plane implements
// it, because the only way to know the seam is right is to build it before the
// second plane (M1) needs it. If engine core ever grows a `switch plane` the
// seam has failed — the engine-core-purity test enforces this mechanically.
package plane

import (
	"context"

	"github.com/artembatutin/grip/internal/ir"
)

// Tier classifies a rule's blocking authority.
type Tier int

const (
	// TierA hard-blocks the gate (exit 1). Architecture's structural rules.
	TierA Tier = iota
	// TierB is advisory-deterministic: reported, non-blocking, but a repo may
	// promote a promotable Tier B rule to Tier A via .grip.yaml.
	TierB
	// TierC is judgment-assisted (may consult an LLM inside Derive, M4). It can
	// never change the exit code — the engine enforces this structurally.
	TierC
)

// String renders a tier for reports and config.
func (t Tier) String() string {
	switch t {
	case TierA:
		return "A"
	case TierB:
		return "B"
	case TierC:
		return "C"
	default:
		return "?"
	}
}

// Kind distinguishes an outright violation from a stale declaration or a
// rendered intentional change, so the reporter can present each appropriately.
type Kind string

const (
	// KindViolation is an accidental drift the gate blocks on.
	KindViolation Kind = "violation"
	// KindStaleDeclaration is a manifest entry with no backing derived reality
	// (symmetric drift, FR-6).
	KindStaleDeclaration Kind = "staleDeclaration"
	// KindIntentionalChange is a change the human authored in a manifest; it is
	// rendered as intentional and never blocks (principle 5).
	KindIntentionalChange Kind = "intentionalChange"
	// KindCannotVerify is a fail-closed result: a rule's evidence sits in a
	// reduced/none-confidence scope, so the gate blocks rather than guess.
	KindCannotVerify Kind = "cannotVerify"
)

// Location points a violation at a place a human can open.
type Location struct {
	Module string `json:"module"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Symbol string `json:"symbol,omitempty"`
}

// Violation is one finding. Message is the single plain sentence — rule, what,
// and remedy (NFR-5) — that the human ultimately reads.
type Violation struct {
	RuleID     string   `json:"ruleId"`
	Plane      string   `json:"plane"`
	Tier       Tier     `json:"tier"`
	Kind       Kind     `json:"kind"`
	Location   Location `json:"location"`
	Message    string   `json:"message"`
	Confidence ir.Level `json:"confidence"`
}

// RuleSpec is the static description of a rule: used by config validation (tier
// promotion must name a real, promotable rule) and by docs generation.
type RuleSpec struct {
	ID         string
	Tier       Tier // default tier
	Promotable bool // may a repo promote B -> A?
	Summary    string
}

// Intent is a plane's parsed, validated slice of a module's manifest. It is
// opaque to the engine; only the owning plane's Reconcile interprets it.
type Intent interface{}

// Derived is a plane's actual-state model. For the Architecture plane it is the
// Common Graph IR; other planes may use their own model.
type Derived interface{}

// ModuleRef identifies a governed module to a plane without exposing loader
// internals. ID is the repo-relative module id (its directory); Path is the
// absolute directory on disk, for the deriver.
type ModuleRef struct {
	ID       string
	Path     string
	Language string
}

// ManifestSection is a plane's raw YAML sub-tree, handed to ParseIntent for
// strict decoding. It is a thin wrapper so the engine need not know YAML types.
type ManifestSection struct {
	// Decode strictly decodes the section into v, rejecting unknown fields
	// (fail-closed on typos inside a governed section).
	Decode func(v interface{}) error
	// Present is false when the module has no section for this plane.
	Present bool
}

// ToolSpec names an analyzer binary and an optional minimum version.
type ToolSpec struct {
	Name       string
	MinVersion string
}

// LanguageSpec tells a deriver where one language's source lives and which tool
// analyzes it. Supplied by the engine from config; the interface stays neutral.
type LanguageSpec struct {
	Language string
	Roots    []string // repo-relative
	Tool     ToolSpec
}

// DeriveServices are the I/O capabilities and engine facts a plane may use inside
// Derive: running external analyzer tools, plus the module topology the engine
// already discovered. Isolating them here is what lets Derive be recorded/mocked
// in tests while Reconcile stays pure.
type DeriveServices struct {
	// Commit is the resolved commit the derivation is for (repo-relative IR
	// carries no timestamps; the engine supplies identity).
	Commit string
	// RepoRoot is the absolute path of the repository root.
	RepoRoot string
	// Tools runs external analyzers. In tests this is backed by recorded output.
	Tools ToolRunner
	// ModuleOf returns the governed module id owning a repo-relative file, or ""
	// if the file is ungoverned. Used to collapse file-level edges to modules.
	ModuleOf func(relFile string) string
	// FilesOf returns the repo-relative source files of a governed module.
	FilesOf func(moduleID string) []string
	// Languages describes each enabled language's roots and analyzer tool.
	Languages []LanguageSpec
	// Layers is the repo-declared layer order (policy.layers.order). Surfaced
	// generically because layered-direction is a language-neutral architectural
	// concept; a plane that ignores it simply does not read it. A plane carries
	// what it needs from here into its Derived model so Reconcile stays pure.
	Layers []string
	// Ungoverned lists the ids of discovered ungoverned modules (a directory
	// with source but no grip.yaml, FR-14). A plane carries this into its model
	// so Reconcile can tell "declared dependency on a module whose manifest is
	// missing" (fail-closed) from "declared dependency on nothing" (stale).
	Ungoverned []string
}

// ToolRunner abstracts running an external analyzer subprocess. The real
// implementation shells out; the test implementation replays recorded output,
// so the deriver's normalization logic is tested offline and deterministically.
type ToolRunner interface {
	// Run executes tool `name` (e.g. "dependency-cruiser") with args and stdin,
	// returning stdout. A missing tool is a fail-closed error (ErrToolMissing).
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, err error)
	// Version returns the resolved version string of tool `name`.
	Version(ctx context.Context, name string) (string, error)
}

// Plane is the extension point. One value per governed axis of drift.
type Plane interface {
	// ID is the stable plane identity, e.g. "architecture".
	ID() string
	// ManifestSection is the top-level key this plane owns in grip.yaml.
	ManifestSection() string
	// ParseIntent parses and validates this plane's slice of one module's
	// manifest, returning a plane-specific Intent or a fail-closed error.
	ParseIntent(raw ManifestSection, mod ModuleRef) (Intent, error)
	// Derive produces this plane's actual-state model for the given modules.
	// All I/O happens here, behind DeriveServices.
	Derive(ctx context.Context, mods []ModuleRef, svc DeriveServices) (Derived, error)
	// Reconcile is the pure heart: (declared, derived) -> violations. NO I/O,
	// deterministic, no map-iteration-order leaks.
	Reconcile(intents map[string]Intent, derived Derived) []Violation
	// Rules statically describes every rule, its tier, and its default, for
	// config validation and docs.
	Rules() []RuleSpec
}
