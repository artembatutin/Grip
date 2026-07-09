// Package gate converts everything into one binary pass/block decision, failing
// closed, honoring tier promotion (plan/02 §3, plan/03 M0.6). It is the generic
// plane loop: it names no plane and branches on none. Fail-closed conditions are
// engine-level and uniform: a missing tool, a derive error, a reduced/none
// confidence touching a rule, an unknown plane, or a malformed manifest all
// block — a false pass is the worst possible bug.
package gate

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/manifest"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/reconcile"
	"github.com/artembatutin/grip/internal/vcs"
)

// Exit codes (plan/01 §8) — stable and scriptable.
const (
	ExitPass       = 0 // clean
	ExitBlocked    = 1 // a real hard violation
	ExitFailClosed = 2 // analysis error / missing tool / missing manifest / cannot-verify
	ExitUsage      = 3 // config or manifest usage error
)

// Options parameterizes a gate run.
type Options struct {
	CI     bool             // authoritative full mode (vs local incremental)
	Planes []string         // explicit plane subset; empty = mode default
	Tools  plane.ToolRunner // injected analyzer runner (recorded in tests)
	Commit string           // override commit identity (tests); else git HEAD
}

// FailClosed is one engine-level reason the gate blocked defensively.
type FailClosed struct {
	Code    string `json:"code"`
	Plane   string `json:"plane,omitempty"`
	Module  string `json:"module,omitempty"`
	Message string `json:"message"`
}

// Outcome is the full, renderable result of a gate run.
type Outcome struct {
	Decision   string            `json:"decision"` // "pass" | "block"
	ExitCode   int               `json:"exitCode"`
	Violations []plane.Violation `json:"violations"`
	FailClosed []FailClosed      `json:"failClosed"`
	Graph      *ir.Graph         `json:"-"`
	Governed   []string          `json:"governed"`
	Ungoverned []string          `json:"ungoverned"`
	Analyzers  []ir.Analyzer     `json:"analyzers"`
	PlanesRun  []string          `json:"planesRun"`
	IRHash     string            `json:"irHash"`
}

// UsageError is a config/manifest authoring error (exit 3), distinct from a
// fail-closed analysis block (exit 2).
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// Run executes the gate for the configured planes and returns the decision. A
// returned error is a usage error (exit 3); every other failure mode is folded
// into the Outcome as a fail-closed block so it can be reported.
func Run(ctx context.Context, cfg *config.Config, reg *plane.Registry, opts Options) (*Outcome, error) {
	repoRoot := cfg.RepoRoot
	commit := opts.Commit
	if commit == "" {
		commit = vcs.HeadCommit(ctx, repoRoot)
	}

	disc, err := manifest.Discover(repoRoot, cfg.LanguageRoots())
	if err != nil {
		return nil, &UsageError{Err: fmt.Errorf("module discovery: %w", err)}
	}

	planesRun, err := resolvePlanes(cfg, reg, opts)
	if err != nil {
		return nil, &UsageError{Err: err}
	}

	svc := plane.DeriveServices{
		Commit:     commit,
		RepoRoot:   repoRoot,
		Tools:      opts.Tools,
		ModuleOf:   disc.ModuleForFile,
		FilesOf:    disc.FilesOf,
		Languages:  cfg.LanguageSpecs(),
		Layers:     append([]string(nil), cfg.Policy.Layers.Order...),
		Ungoverned: disc.UngovernedIDs(),
	}

	out := &Outcome{
		Decision:   "pass",
		Governed:   disc.GovernedIDs(),
		Ungoverned: disc.UngovernedIDs(),
		PlanesRun:  planesRun,
	}

	for _, id := range planesRun {
		p, _ := reg.Get(id) // resolvePlanes guaranteed existence
		mods := planeModules(p, disc)
		res, rerr := reconcile.RunPlane(ctx, p, mods, svc)
		if rerr != nil {
			var ie *reconcile.IntentError
			if errors.As(rerr, &ie) {
				// Malformed manifest section: a human authoring error (exit 3).
				return nil, &UsageError{Err: rerr}
			}
			var tm *plane.ToolMissingError
			if errors.As(rerr, &tm) {
				out.FailClosed = append(out.FailClosed, FailClosed{
					Code: "tool-missing", Plane: id, Message: tm.Error(),
				})
				continue
			}
			out.FailClosed = append(out.FailClosed, FailClosed{
				Code: "derive-error", Plane: id, Message: rerr.Error(),
			})
			continue
		}
		out.Violations = append(out.Violations, res.Violations...)
		// Capture the architecture graph for diff/report/version when present.
		if g := extractGraph(res); g != nil {
			out.Graph = g
		}
	}

	if out.Graph != nil {
		out.Analyzers = out.Graph.Analyzers
		out.IRHash = out.Graph.Hash()
	}

	promoted := cfg.PromotedRules()
	decide(out, promoted)
	SortViolations(out.Violations)
	return out, nil
}

// decide sets Decision and ExitCode from the aggregated violations and
// fail-closed reasons, applying tier promotion.
func decide(out *Outcome, promoted map[string]bool) {
	failClosed := len(out.FailClosed) > 0
	hardBlock := false
	for _, v := range out.Violations {
		if v.Tier == plane.TierC {
			// Tier C is judgment-assisted (the only place an LLM enters Grip) and
			// is structurally excluded from the gate decision: it can neither block
			// nor fail-closed, regardless of any promotion (principle 3, NFR-1). It
			// is reported, never gated. Config also refuses to promote a Tier C rule
			// (defence in depth), but this skip is the load-bearing guarantee.
			continue
		}
		if v.Kind == plane.KindCannotVerify {
			failClosed = true
			continue
		}
		if v.Kind == plane.KindIntentionalChange {
			continue
		}
		if v.Tier == plane.TierA || promoted[v.RuleID] {
			hardBlock = true
		}
	}
	switch {
	case failClosed:
		out.Decision = "block"
		out.ExitCode = ExitFailClosed
	case hardBlock:
		out.Decision = "block"
		out.ExitCode = ExitBlocked
	default:
		out.Decision = "pass"
		out.ExitCode = ExitPass
	}
}

// resolvePlanes returns the sorted plane ids to run, validating that each is
// enabled in config and registered in the binary (fail-closed on unknown).
func resolvePlanes(cfg *config.Config, reg *plane.Registry, opts Options) ([]string, error) {
	enabled := map[string]bool{}
	for _, id := range cfg.EnabledPlanes() {
		enabled[id] = true
	}
	var want []string
	if len(opts.Planes) > 0 {
		want = opts.Planes
	} else {
		want = cfg.PlanesForMode(opts.CI)
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range want {
		if !enabled[id] {
			return nil, fmt.Errorf("plane %q is not enabled in %s", id, config.Filename)
		}
		if _, ok := reg.Get(id); !ok {
			return nil, fmt.Errorf("plane %q is enabled but not registered in this binary", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// planeModules builds the reconcile inputs for a plane: every governed module
// paired with its section for that plane.
func planeModules(p plane.Plane, disc *manifest.Discovery) []reconcile.Module {
	section := p.ManifestSection()
	mods := make([]reconcile.Module, 0, len(disc.Governed))
	for _, m := range disc.Governed {
		mods = append(mods, reconcile.Module{
			Ref:     plane.ModuleRef{ID: m.ID, Path: m.Dir, Language: m.Language},
			Section: m.Manifest.Section(section),
		})
	}
	sort.Slice(mods, func(a, b int) bool { return mods[a].Ref.ID < mods[b].Ref.ID })
	return mods
}

// extractGraph pulls an *ir.Graph out of a plane result if the plane exposes one
// via the GraphProvider convention. Keeping this behind an interface avoids the
// gate importing any specific plane (purity).
func extractGraph(res *reconcile.Result) *ir.Graph {
	if res == nil {
		return nil
	}
	if gp, ok := res.Derived.(GraphProvider); ok {
		return gp.IRGraph()
	}
	return nil
}

// GraphProvider is implemented by a plane's Derived model that carries a Common
// Graph IR, so the gate can surface it for diff/report/version without knowing
// the plane. The architecture plane's Model implements it.
type GraphProvider interface {
	IRGraph() *ir.Graph
}
