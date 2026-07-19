package derive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/artembatutin/grip/ci/helpers"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
	"github.com/artembatutin/grip/internal/toolversion"
)

// Deriver produces the IR for one language by invoking that language's bundled
// helper (which wraps the ecosystem analyzers) and normalizing the report.
type Deriver interface {
	Language() string
	Derive(ctx context.Context, spec plane.LanguageSpec, svc plane.DeriveServices, moduleIDs []string) (*ir.Graph, error)
}

// RunHelper is the shared body of every deriver: run the helper for a language,
// parse the AnalyzerReport, and normalize it. helperName is the logical tool the
// ToolRunner resolves ("typescript" / "php"); the recorded runner keys on it and
// the exec runner maps it to the real bundled script. Language derivers live in
// their own packages and call this, so the parent package never imports them
// (no cycle) — the seam that keeps adding a language a leaf change (D9).
func RunHelper(ctx context.Context, helperName, language string, spec plane.LanguageSpec, svc plane.DeriveServices, moduleIDs []string) (*ir.Graph, error) {
	args := helperArgs(spec, svc)
	out, err := svc.Tools.Run(ctx, helperName, args, nil)
	if err != nil {
		// Missing tool / analyzer error is fail-closed; propagate verbatim so the
		// gate can attach the right exit code and install hint (NFR-6).
		return nil, err
	}
	var rep AnalyzerReport
	if err := json.Unmarshal(out, &rep); err != nil {
		return nil, fmt.Errorf("%s deriver: parse analyzer report: %w", language, err)
	}
	if err := ValidateReport(language, spec.Tool, &rep); err != nil {
		return nil, err
	}
	return Normalize(language, &rep, moduleIDs, svc.ModuleOf, svc.FilesOf, svc.UngovernedOf)
}

// ValidateReport rejects incomplete or self-contradictory helper output before
// it reaches the deterministic IR. In particular, a helper cannot claim a
// configured analyzer it did not actually run, and unknown confidence is never
// silently downgraded into a passable result.
func ValidateReport(language string, configured plane.ToolSpec, rep *AnalyzerReport) error {
	if rep == nil {
		return fmt.Errorf("%s deriver: empty analyzer report", language)
	}
	if configured.Name == "" {
		return fmt.Errorf("%s deriver: analyzer is not configured", language)
	}
	if rep.Tool.Name != configured.Name {
		return fmt.Errorf("%s deriver: configured analyzer %q but helper reported %q", language, configured.Name, rep.Tool.Name)
	}
	if _, err := toolversion.Parse(rep.Tool.Version); err != nil {
		return fmt.Errorf("%s deriver: analyzer %q returned an unverifiable version %q: %w", language, rep.Tool.Name, rep.Tool.Version, err)
	}
	if configured.MinVersion != "" {
		actual, _ := toolversion.Parse(rep.Tool.Version)
		minimum, err := toolversion.Parse(configured.MinVersion)
		if err != nil {
			return fmt.Errorf("%s deriver: invalid minimum version %q: %w", language, configured.MinVersion, err)
		}
		if toolversion.Compare(actual, minimum) < 0 {
			return fmt.Errorf("%s deriver: analyzer %q version %s is below required minimum %s", language, rep.Tool.Name, rep.Tool.Version, configured.MinVersion)
		}
	}
	if rep.SurfaceTool.Name == "" || rep.SurfaceTool.Version == "" {
		return fmt.Errorf("%s deriver: missing surface analyzer identity", language)
	}
	for _, im := range rep.Imports {
		if im.FromFile == "" || im.ToFile == "" || im.Symbol == "" || im.Line < 1 {
			return fmt.Errorf("%s deriver: malformed import evidence", language)
		}
		switch im.Kind {
		case "", "import", "require", "re-export", "call", "extends", "implements", "trait-use", "static-reference", "constructor-type":
		default:
			return fmt.Errorf("%s deriver: unknown edge kind %q", language, im.Kind)
		}
	}
	for _, ex := range rep.Exports {
		if ex.File == "" || ex.Name == "" || ex.Kind == "" || ex.Line < 1 {
			return fmt.Errorf("%s deriver: malformed export evidence", language)
		}
	}
	for _, reduced := range rep.Reduced {
		if reduced.File == "" || reduced.Reason == "" {
			return fmt.Errorf("%s deriver: malformed reduced-confidence evidence", language)
		}
		if reduced.Level != "" && reduced.Level != string(ir.LevelReduced) && reduced.Level != string(ir.LevelNone) {
			return fmt.Errorf("%s deriver: unknown confidence level %q", language, reduced.Level)
		}
	}
	return nil
}

// helperArgs builds the argument list passed to a helper: the repo root and the
// language roots to analyze.
func helperArgs(spec plane.LanguageSpec, svc plane.DeriveServices) []string {
	args := []string{"--repo-root", svc.RepoRoot}
	for _, r := range spec.Roots {
		args = append(args, "--root", r)
	}
	return args
}

// ExecRunner is the production ToolRunner. It runs the first-party helper assets
// embedded in every Grip binary (or an explicit GRIP_HELPER_DIR override), so a
// consumer repository never needs to carry copied helper scripts.
type ExecRunner struct {
	// HelperDir holds the bundled helper scripts (ts.mjs, php.php).
	HelperDir string
	// RepoRoot is where subprocesses run.
	RepoRoot string
}

// helperRuntime maps a logical helper to its runtime binary and script.
func (r *ExecRunner) helperRuntime(name string) (bin, script, installHint string, ok bool) {
	dir := r.HelperDir
	if dir == "" {
		var err error
		dir, err = helpers.Directory()
		if err != nil {
			return "", "", err.Error(), true
		}
	}
	switch name {
	case "typescript":
		return "node", filepath.Join(dir, "ts.mjs"),
			"install Node.js and run `npm i -g dependency-cruiser` (+ ts-morph in the helper)", true
	case "php":
		return "php", filepath.Join(dir, "php.php"),
			"install PHP and `composer global require qossmic/deptrac nikic/php-parser`", true
	default:
		return "", "", "", false
	}
}

// Run executes the bundled helper for a language and returns its stdout.
func (r *ExecRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	bin, script, hint, ok := r.helperRuntime(name)
	if !ok {
		return nil, fmt.Errorf("derive: unknown helper %q", name)
	}
	if script == "" {
		return nil, &plane.ToolMissingError{Tool: name + " helper", Hint: hint}
	}
	if info, err := os.Stat(script); err != nil || info.IsDir() {
		return nil, &plane.ToolMissingError{Tool: name + " helper", Hint: "embedded helper unavailable at " + script + "; unset GRIP_HELPER_DIR or reinstall Grip"}
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, &plane.ToolMissingError{Tool: bin, Hint: hint}
	}
	full := append([]string{script}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Dir = r.RepoRoot
	cmd.Stdin = strings.NewReader(string(stdin))
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if ok := asExit(err, &ee); ok && ee.ExitCode() == toolMissingExit {
			return nil, &plane.ToolMissingError{Tool: name, Hint: hint}
		}
		return nil, fmt.Errorf("derive: helper %q failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Version returns the resolved version of a runtime; helpers embed analyzer
// versions in their report, so this is only used for the runtime itself.
func (r *ExecRunner) Version(ctx context.Context, name string) (string, error) {
	// Analyzer probes are separate from helper runtimes. Keeping them here means
	// `grip version` reports facts discovered on this machine, never configured
	// labels masquerading as versions.
	if bin, args, ok := analyzerVersionCommand(name); ok {
		if _, err := exec.LookPath(bin); err != nil {
			return "", &plane.ToolMissingError{Tool: bin, Hint: "install the configured analyzer " + name}
		}
		out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("derive: resolve %s version: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	bin, _, _, ok := r.helperRuntime(name)
	if !ok {
		return "", fmt.Errorf("derive: unknown helper %q", name)
	}
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func analyzerVersionCommand(name string) (string, []string, bool) {
	switch name {
	case "dependency-cruiser":
		return "depcruise", []string{"--version"}, true
	case "deptrac":
		return "deptrac", []string{"--version"}, true
	case "stryker":
		return "stryker", []string{"--version"}, true
	case "infection":
		return "infection", []string{"--version"}, true
	default:
		return "", nil, false
	}
}

// toolMissingExit is the exit code a helper uses to signal "my analyzer isn't
// installed" (distinct from a genuine analysis failure).
const toolMissingExit = 3

func asExit(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// RecordedRunner is the offline ToolRunner used by tests and the acceptance
// harness. It returns recorded analyzer reports read from a directory
// (<AnalysisDir>/<name>.json), so the deriver's normalization runs against real
// tool-shaped output without the tools installed (plan/08 §1).
type RecordedRunner struct {
	AnalysisDir string
	// Missing, when set for a helper name, forces a ToolMissingError to exercise
	// the fail-closed path.
	Missing map[string]string // name -> install hint
}

// Run returns the recorded report for a helper, or a fail-closed error.
func (r *RecordedRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	if hint, ok := r.Missing[name]; ok {
		return nil, &plane.ToolMissingError{Tool: name, Hint: hint}
	}
	path := filepath.Join(r.AnalysisDir, name+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A language configured but with no recorded report contributes no
			// modules (the repo may not use it). Return an empty report.
			return json.Marshal(AnalyzerReport{})
		}
		return nil, fmt.Errorf("recorded runner: %w", err)
	}
	return b, nil
}

// Version returns a stable recorded version.
func (r *RecordedRunner) Version(ctx context.Context, name string) (string, error) {
	return "recorded", nil
}
