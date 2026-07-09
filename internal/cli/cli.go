// Package cli is Grip's command surface (plan/03 M0.8, GR-X-1). It uses a small
// stdlib subcommand router rather than cobra: cobra is not fetchable in this
// offline environment, and a dependency-light single static binary (only
// gopkg.in/yaml.v3) better serves D1. The command set and flags mirror the plan.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/derive/php"
	"github.com/artembatutin/grip/internal/derive/typescript"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/plane/architecture"
	"github.com/artembatutin/grip/internal/plane/behavior"
	"github.com/artembatutin/grip/internal/plane/contract"
	"github.com/artembatutin/grip/internal/plane/testrigor"
)

// Version is Grip's release version, printed by `grip version` and stamped into
// reports for reproducibility.
const Version = "0.1.0-m0"

// exit codes shared with the gate.
const (
	exitOK    = 0
	exitUsage = 3
)

// App holds the runtime wiring and output streams.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Reg    *plane.Registry
}

// BuildOrchestrator wires the concrete language derivers. This and BuildRegistry
// are the only places that name languages/planes.
func BuildOrchestrator() *derive.Orchestrator {
	return derive.NewOrchestrator(typescript.New(), php.New())
}

// BuildRegistry is the single wiring point that knows the concrete planes and
// language derivers. Everything else reaches planes only through the registry —
// this is what the engine-core-purity test protects. Adding the M1 test-rigor
// plane is exactly one line here plus its package: no engine change (plan/04).
func BuildRegistry() *plane.Registry {
	reg := plane.NewRegistry()
	reg.Register(architecture.New(BuildOrchestrator()))
	reg.Register(testrigor.New(nil)) // nil → default filesystem mutation cache
	reg.Register(behavior.New())     // M2 snapshot+baseline plane
	reg.Register(contract.New())     // M3 versioned/temporal contract plane
	return reg
}

// Main runs a Grip invocation and returns the process exit code.
func Main(args []string) int {
	app := &App{Stdout: os.Stdout, Stderr: os.Stderr, Reg: BuildRegistry()}
	return app.Run(args)
}

// Run dispatches a subcommand.
func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	ctx := context.Background()
	switch cmd {
	case "version", "--version", "-v":
		return a.cmdVersion(rest)
	case "gate":
		return a.cmdGate(ctx, rest)
	case "derive":
		return a.cmdDerive(ctx, rest)
	case "diff":
		return a.cmdDiff(ctx, rest)
	case "view":
		return a.cmdView(ctx, rest)
	case "modules":
		return a.cmdModules(ctx, rest)
	case "init":
		return a.cmdInit(ctx, rest)
	case "ratify":
		return a.cmdRatify(ctx, rest)
	case "help", "--help", "-h":
		a.usage()
		return exitOK
	default:
		fmt.Fprintf(a.Stderr, "grip: unknown command %q\n\n", cmd)
		a.usage()
		return exitUsage
	}
}

func (a *App) usage() {
	fmt.Fprint(a.Stderr, `grip — a deterministic control plane that keeps a human the architect.

usage: grip <command> [flags]

commands:
  gate      derive, reconcile, and decide pass/block for the repo
  derive    dump the derived Common Graph IR (debug)
  diff      show the shape delta vs a baseline snapshot
  view      render a read-only HTML viewer of the derived graph + shape diff
  modules   list governed and ungoverned modules
  init      scaffold .grip.yaml and draft grip.yaml manifests
  ratify    accept current derived state as the baseline
            (ratify behavior <module> re-pins that module's behavior snapshots;
             ratify contract <module> adopts that module's current wire contracts)
  version   print grip and resolved analyzer versions

run "grip <command> --help" for command flags.
`)
}

// resolveRepoRoot finds the nearest ancestor directory containing .grip.yaml,
// starting at start. Returns a usage error if none is found.
func resolveRepoRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, config.Filename)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or any parent directory", config.Filename, start)
		}
		dir = parent
	}
}

// toolRunner returns the analyzer runner: a RecordedRunner over analysisDir when
// set (offline: recorded reports), else an ExecRunner over the real tools.
func toolRunner(repoRoot, analysisDir string) plane.ToolRunner {
	if analysisDir != "" {
		return &derive.RecordedRunner{AnalysisDir: analysisDir}
	}
	helperDir := os.Getenv("GRIP_HELPER_DIR")
	if helperDir == "" {
		helperDir = filepath.Join(repoRoot, ".grip", "helpers")
	}
	return &derive.ExecRunner{HelperDir: helperDir, RepoRoot: repoRoot}
}

// cwd returns the working directory or ".".
func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}
