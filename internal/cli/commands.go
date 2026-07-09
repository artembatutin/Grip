package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/diff"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/manifest"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/plane/architecture"
	"github.com/artembatutin/grip/internal/plane/behavior"
	"github.com/artembatutin/grip/internal/plane/contract"
	"github.com/artembatutin/grip/internal/ratify"
	"github.com/artembatutin/grip/internal/report"
)

// baselineRelPath is where ratify writes and diff reads the baseline snapshot.
var baselineRelPath = filepath.Join(".grip", "baseline.json")

func (a *App) cmdVersion(args []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	fmt.Fprintf(a.Stdout, "grip %s\n", Version)
	fmt.Fprintf(a.Stdout, "ir schema: %s\n", ir.Version)
	// Analyzer versions are resolved per run and captured in the IR/report; the
	// configured tools are shown when a repo config is reachable.
	if root, err := resolveRepoRoot(cwd()); err == nil {
		if cfg, err := config.Load(root, a.Reg); err == nil {
			langs := cfg.LanguageRoots()
			for _, l := range langs {
				fmt.Fprintf(a.Stdout, "analyzer[%s]: %s\n", l.Language, cfg.Languages[l.Language].Tool.Name)
			}
		}
	}
	return exitOK
}

func (a *App) cmdGate(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	local := fs.Bool("local", false, "fast local (incremental) mode")
	ci := fs.Bool("ci", false, "authoritative CI (full) mode")
	planeName := fs.String("plane", "", "run only this plane")
	asJSON := fs.Bool("json", false, "emit JSON report")
	asSARIF := fs.Bool("sarif", false, "emit SARIF report")
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	opts := gate.Options{
		CI:     *ci && !*local,
		Tools:  toolRunner(root, *analysisDir),
		Commit: os.Getenv("GRIP_COMMIT"),
	}
	if *planeName != "" {
		opts.Planes = []string{*planeName}
	}
	out, err := gate.Run(ctx, cfg, a.Reg, opts)
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}

	view := report.View{Outcome: out}
	if d := a.deltaAgainstBaseline(root, cfg, out); d != nil {
		view.Delta = d
	}
	if *asJSON {
		// The JSON report is the document the read-only viewer consumes, so make it
		// a superset: attach the declared surfaces for the allowed-vs-actual overlay.
		view.Declared = a.declaredSurfaces(root, cfg)
	}
	if code := a.render(view, *asJSON, *asSARIF); code != exitOK {
		return code
	}
	return out.ExitCode
}

// cmdView renders the read-only visualization (M4 Part B, GR-X-7): a self-contained
// static HTML page over the gate's JSON report — the derived graph with manifest
// overlays plus the shape diff. It is STRICTLY read-only and derived: it consumes
// the same report model as `--json` and offers no affordance to change a manifest,
// edge, or facade. It never gates, so it returns exit 0 on success regardless of
// the decision it renders.
func (a *App) cmdView(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	asJSON := fs.Bool("json", false, "emit the underlying JSON report instead of HTML")
	outPath := fs.String("o", "", "write the viewer HTML to this file (default: stdout)")
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	out, err := gate.Run(ctx, cfg, a.Reg, gate.Options{Tools: toolRunner(root, *analysisDir), Commit: os.Getenv("GRIP_COMMIT")})
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	view := report.View{Outcome: out, Declared: a.declaredSurfaces(root, cfg)}
	if d := a.deltaAgainstBaseline(root, cfg, out); d != nil {
		view.Delta = d
	}
	if *asJSON {
		b, err := report.JSON(view)
		if err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitFailClosed
		}
		a.Stdout.Write(b)
		return exitOK
	}
	htmlDoc := report.HTML(report.BuildDocument(view))
	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(htmlDoc), 0o644); err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitUsage
		}
		fmt.Fprintf(a.Stderr, "grip: wrote read-only viewer to %s\n", *outPath)
		return exitOK
	}
	fmt.Fprint(a.Stdout, htmlDoc)
	return exitOK
}

func (a *App) cmdDerive(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("derive", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	out, err := gate.Run(ctx, cfg, a.Reg, gate.Options{Tools: toolRunner(root, *analysisDir), Commit: os.Getenv("GRIP_COMMIT")})
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	if out.Graph == nil {
		fmt.Fprintln(a.Stderr, "grip: no IR derived (no architecture plane enabled?)")
		return gate.ExitFailClosed
	}
	b, err := out.Graph.Canonical()
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitFailClosed
	}
	a.Stdout.Write(b)
	return exitOK
}

func (a *App) cmdModules(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("modules", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	disc, err := manifest.Discover(root, cfg.LanguageRoots())
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	if *asJSON {
		payload := map[string]interface{}{
			"governed":   disc.GovernedIDs(),
			"ungoverned": disc.UngovernedIDs(),
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Fprintln(a.Stdout, string(b))
		return exitOK
	}
	fmt.Fprintf(a.Stdout, "governed modules (%d):\n", len(disc.Governed))
	for _, m := range disc.Governed {
		fmt.Fprintf(a.Stdout, "  %s [%s]\n", m.ID, m.Language)
	}
	fmt.Fprintf(a.Stdout, "ungoverned modules (%d):\n", len(disc.Ungoverned))
	for _, m := range disc.Ungoverned {
		fmt.Fprintf(a.Stdout, "  %s [%s] — no grip.yaml\n", m.ID, m.Language)
	}
	return exitOK
}

func (a *App) cmdDiff(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	baseline := fs.String("baseline", "", "path to a baseline snapshot (default .grip/baseline.json)")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	basePath := *baseline
	if basePath == "" {
		basePath = filepath.Join(root, baselineRelPath)
	}
	before, err := loadSnapshot(basePath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: no baseline to diff against (%v); run `grip ratify` first\n", err)
		return gate.ExitUsage
	}
	out, err := gate.Run(ctx, cfg, a.Reg, gate.Options{Tools: toolRunner(root, *analysisDir), Commit: os.Getenv("GRIP_COMMIT")})
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	after := a.currentSnapshot(root, cfg, out.Graph)
	d := diff.Compute(before, after)
	if *asJSON {
		b, _ := json.MarshalIndent(d, "", "  ")
		fmt.Fprintln(a.Stdout, string(b))
		return exitOK
	}
	if d.Empty() {
		fmt.Fprintln(a.Stdout, "grip: no shape change vs baseline.")
		return exitOK
	}
	view := report.View{Outcome: out, Delta: d}
	fmt.Fprint(a.Stdout, report.Human(view))
	return exitOK
}

func (a *App) cmdInit(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	write := fs.Bool("write", false, "write generated files (default: dry-run print)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root := cwd()
	if r, err := resolveRepoRoot(root); err == nil {
		root = r
	}
	cfg, cfgErr := config.Load(root, a.Reg)
	if cfgErr != nil {
		fmt.Fprintf(a.Stderr, "grip init: no usable %s yet; write one first or run in a configured repo (%v)\n", config.Filename, cfgErr)
		return gate.ExitUsage
	}
	// Onboarding derives from CANDIDATE modules (immediate children of roots
	// with source), so it works on a repo with zero grip.yaml files yet.
	cand, err := manifest.Candidates(root, cfg.LanguageRoots())
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	svc := plane.DeriveServices{
		Commit:    os.Getenv("GRIP_COMMIT"),
		RepoRoot:  root,
		Tools:     toolRunner(root, *analysisDir),
		ModuleOf:  cand.ModuleOf,
		FilesOf:   cand.FilesOf,
		Languages: cfg.LanguageSpecs(),
	}
	g, err := BuildOrchestrator().Derive(ctx, cand.Refs(), svc)
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip init: %v\n", err)
		return gate.ExitFailClosed
	}
	files := ratify.DraftManifests(g)
	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		if *write {
			if _, err := os.Stat(abs); err == nil {
				fmt.Fprintf(a.Stdout, "skip (exists): %s\n", f.Path)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				fmt.Fprintf(a.Stderr, "grip: %v\n", err)
				return gate.ExitUsage
			}
			if err := os.WriteFile(abs, []byte(f.Content), 0o644); err != nil {
				fmt.Fprintf(a.Stderr, "grip: %v\n", err)
				return gate.ExitUsage
			}
			fmt.Fprintf(a.Stdout, "wrote draft: %s\n", f.Path)
		} else {
			fmt.Fprintf(a.Stdout, "--- %s ---\n%s\n", f.Path, f.Content)
		}
	}
	return exitOK
}

func (a *App) cmdRatify(ctx context.Context, args []string) int {
	// `grip ratify behavior <module>` re-pins the behavior plane's snapshots for a
	// module (M2); `grip ratify contract <module>` adopts the contract plane's
	// current wire contracts (M3); the bare `grip ratify` accepts the architecture
	// baseline (M0).
	if len(args) >= 1 && args[0] == "behavior" {
		return a.cmdRatifyBehavior(ctx, args[1:])
	}
	if len(args) >= 1 && args[0] == "contract" {
		return a.cmdRatifyContract(ctx, args[1:])
	}
	fs := flag.NewFlagSet("ratify", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	out, err := gate.Run(ctx, cfg, a.Reg, gate.Options{Tools: toolRunner(root, *analysisDir), Commit: os.Getenv("GRIP_COMMIT")})
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	if out.Graph == nil {
		fmt.Fprintln(a.Stderr, "grip ratify: nothing derived")
		return gate.ExitFailClosed
	}
	snap := a.currentSnapshot(root, cfg, out.Graph)
	abs := filepath.Join(root, baselineRelPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	if err := os.WriteFile(abs, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	fmt.Fprintf(a.Stdout, "grip: baseline written to %s (gate: %s)\n", baselineRelPath, out.Decision)
	return exitOK
}

// cmdRatifyBehavior re-pins the behavior plane's boundary snapshots for one
// module (M2, GR-BEH-1). It captures the module's current observable behavior and
// writes the normalized snapshot files under <module>/.grip/behavior/; that
// committed edit IS the recorded design decision the gate later reads as the
// ratification. Nondeterministic boundaries are refused (fail-closed): an unstable
// snapshot must never be pinned.
func (a *App) cmdRatifyBehavior(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ratify behavior", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	moduleID := fs.Arg(0)
	if moduleID == "" {
		fmt.Fprintln(a.Stderr, "usage: grip ratify behavior <module>")
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	disc, err := manifest.Discover(root, cfg.LanguageRoots())
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	target := disc.GovernedModule(moduleID)
	if target == nil {
		fmt.Fprintf(a.Stderr, "grip ratify behavior: %q is not a governed module (governed: %s)\n", moduleID, strings.Join(disc.GovernedIDs(), ", "))
		return gate.ExitUsage
	}

	refs := make([]plane.ModuleRef, 0, len(disc.Governed))
	for _, m := range disc.Governed {
		refs = append(refs, plane.ModuleRef{ID: m.ID, Path: m.Dir, Language: m.Language})
	}
	svc := plane.DeriveServices{
		Commit:    os.Getenv("GRIP_COMMIT"),
		RepoRoot:  root,
		Tools:     toolRunner(root, *analysisDir),
		ModuleOf:  disc.ModuleForFile,
		FilesOf:   disc.FilesOf,
		Languages: cfg.LanguageSpecs(),
	}
	derived, err := behavior.New().Derive(ctx, refs, svc)
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip ratify behavior: %v\n", err)
		return gate.ExitFailClosed
	}

	// Pin the module's declared boundaries (behavior.pin); with none declared,
	// accept all observed boundaries as the current reality.
	var filter map[string]bool
	if in, err := behavior.New().ParseIntent(target.Manifest.Section(behavior.PlaneID), plane.ModuleRef{ID: moduleID}); err == nil {
		if bi, ok := in.(behavior.Intent); ok && len(bi.Pin) > 0 {
			filter = map[string]bool{}
			for _, b := range bi.Pin {
				filter[b] = true
			}
		}
	}

	files := behavior.SnapshotsFor(derived, moduleID, filter)
	if len(files) == 0 {
		fmt.Fprintf(a.Stdout, "grip: no observable boundaries captured for %s — nothing to pin.\n", moduleID)
		return exitOK
	}
	wrote, reduced := 0, 0
	for _, f := range files {
		if f.Reduced {
			reduced++
			fmt.Fprintf(a.Stderr, "grip: refusing to pin nondeterministic boundary %s (stabilize it first)\n", f.Boundary)
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitUsage
		}
		if err := os.WriteFile(abs, []byte(f.Content), 0o644); err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitUsage
		}
		wrote++
		fmt.Fprintf(a.Stdout, "pinned %s\n", f.Path)
	}
	fmt.Fprintf(a.Stdout, "grip: re-pinned %d boundary snapshot(s) for %s.\n", wrote, moduleID)
	if reduced > 0 {
		// Fail-closed: the human asked to pin a boundary we cannot capture stably.
		return gate.ExitFailClosed
	}
	return exitOK
}

// cmdRatifyContract adopts a module's currently-derived wire contracts as the
// declared baseline (M3, GR-CON-1; reuses generate-then-ratify, M0.10). It derives
// the module's current api/event/db contracts and writes the canonical baseline
// artifacts under <module>/.grip/contract/; that committed edit IS the recorded
// design decision a later gate reads as the ratification. A kind whose current
// shape could not be derived is refused (fail-closed): never adopt a contract Grip
// cannot see.
func (a *App) cmdRatifyContract(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ratify contract", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	analysisDir := fs.String("analysis-dir", "", "use recorded analyzer reports from this dir (offline)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	moduleID := fs.Arg(0)
	if moduleID == "" {
		fmt.Fprintln(a.Stderr, "usage: grip ratify contract <module>")
		return exitUsage
	}
	root, cfg, code := a.loadRepo()
	if code != exitOK {
		return code
	}
	disc, err := manifest.Discover(root, cfg.LanguageRoots())
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return gate.ExitUsage
	}
	target := disc.GovernedModule(moduleID)
	if target == nil {
		fmt.Fprintf(a.Stderr, "grip ratify contract: %q is not a governed module (governed: %s)\n", moduleID, strings.Join(disc.GovernedIDs(), ", "))
		return gate.ExitUsage
	}

	refs := make([]plane.ModuleRef, 0, len(disc.Governed))
	for _, m := range disc.Governed {
		refs = append(refs, plane.ModuleRef{ID: m.ID, Path: m.Dir, Language: m.Language})
	}
	svc := plane.DeriveServices{
		Commit:    os.Getenv("GRIP_COMMIT"),
		RepoRoot:  root,
		Tools:     toolRunner(root, *analysisDir),
		ModuleOf:  disc.ModuleForFile,
		FilesOf:   disc.FilesOf,
		Languages: cfg.LanguageSpecs(),
	}
	derived, err := contract.New().Derive(ctx, refs, svc)
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip ratify contract: %v\n", err)
		return gate.ExitFailClosed
	}

	// Adopt the module's governed kinds (contract.<kind>); with none parseable,
	// accept every kind whose contract could be derived.
	var filter map[string]bool
	if in, err := contract.New().ParseIntent(target.Manifest.Section(contract.PlaneID), plane.ModuleRef{ID: moduleID}); err == nil {
		if ci, ok := in.(contract.Intent); ok {
			if kinds := ci.GovernedKinds(); len(kinds) > 0 {
				filter = map[string]bool{}
				for _, k := range kinds {
					filter[k] = true
				}
			}
		}
	}

	files := contract.BaselinesFor(derived, moduleID, filter)
	if len(files) == 0 {
		fmt.Fprintf(a.Stdout, "grip: no derivable contracts for %s — nothing to adopt.\n", moduleID)
		return exitOK
	}
	wrote, missing := 0, 0
	for _, f := range files {
		if f.Missing {
			missing++
			fmt.Fprintf(a.Stderr, "grip: refusing to adopt %s contract — its current shape could not be derived\n", f.Kind)
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitUsage
		}
		if err := os.WriteFile(abs, []byte(f.Content), 0o644); err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitUsage
		}
		wrote++
		fmt.Fprintf(a.Stdout, "adopted %s\n", f.Path)
	}
	fmt.Fprintf(a.Stdout, "grip: adopted %d contract baseline(s) for %s.\n", wrote, moduleID)
	if missing > 0 {
		// Fail-closed: the human asked to adopt a contract we could not derive.
		return gate.ExitFailClosed
	}
	return exitOK
}

// --- helpers ---

func (a *App) loadRepo() (root string, cfg *config.Config, code int) {
	root, err := resolveRepoRoot(cwd())
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return "", nil, gate.ExitUsage
	}
	cfg, err = config.Load(root, a.Reg)
	if err != nil {
		fmt.Fprintf(a.Stderr, "grip: %v\n", err)
		return "", nil, gate.ExitUsage
	}
	return root, cfg, exitOK
}

func (a *App) render(v report.View, asJSON, asSARIF bool) int {
	switch {
	case asJSON:
		b, err := report.JSON(v)
		if err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitFailClosed
		}
		a.Stdout.Write(b)
	case asSARIF:
		b, err := report.SARIF(v)
		if err != nil {
			fmt.Fprintf(a.Stderr, "grip: %v\n", err)
			return gate.ExitFailClosed
		}
		a.Stdout.Write(b)
	default:
		fmt.Fprint(a.Stdout, report.Human(v))
	}
	return exitOK
}

// currentSnapshot builds a diff.Input from the derived graph plus the declared
// surfaces read from each governed module's manifest.
func (a *App) currentSnapshot(root string, cfg *config.Config, g *ir.Graph) diff.Input {
	in := diff.Input{Graph: g, Facades: map[string][]string{}, Allows: map[string][]string{}}
	disc, err := manifest.Discover(root, cfg.LanguageRoots())
	if err != nil {
		return in
	}
	for _, m := range disc.Governed {
		facade, allow := architecture.DeclaredSurface(m.Manifest.Section(architecture.PlaneID), m.ID)
		if len(facade) > 0 {
			sort.Strings(facade)
			in.Facades[m.ID] = facade
		}
		if len(allow) > 0 {
			sort.Strings(allow)
			in.Allows[m.ID] = allow
		}
	}
	return in
}

// declaredSurfaces reads each governed module's declared facade and allowed
// dependencies into plain report.Surface data, for the viewer's allowed-vs-actual
// overlay. The CLI is the wiring point, so it may name the architecture plane here;
// the report package stays plane-agnostic and receives only the resulting bytes.
func (a *App) declaredSurfaces(root string, cfg *config.Config) map[string]report.Surface {
	out := map[string]report.Surface{}
	disc, err := manifest.Discover(root, cfg.LanguageRoots())
	if err != nil {
		return out
	}
	for _, m := range disc.Governed {
		facade, allow := architecture.DeclaredSurface(m.Manifest.Section(architecture.PlaneID), m.ID)
		if len(facade) == 0 && len(allow) == 0 {
			continue
		}
		sort.Strings(facade)
		sort.Strings(allow)
		out[m.ID] = report.Surface{Facade: facade, Allow: allow}
	}
	return out
}

func (a *App) deltaAgainstBaseline(root string, cfg *config.Config, out *gate.Outcome) *diff.Delta {
	if out.Graph == nil {
		return nil
	}
	before, err := loadSnapshot(filepath.Join(root, baselineRelPath))
	if err != nil {
		return nil // no baseline yet — gate simply omits the delta
	}
	after := a.currentSnapshot(root, cfg, out.Graph)
	d := diff.Compute(before, after)
	if d.Empty() {
		return nil
	}
	return d
}

func loadSnapshot(path string) (diff.Input, error) {
	var in diff.Input
	b, err := os.ReadFile(path)
	if err != nil {
		return in, err
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, err
	}
	return in, nil
}
