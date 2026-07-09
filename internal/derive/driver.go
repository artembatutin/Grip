package derive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
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
	return Normalize(language, &rep, moduleIDs, svc.ModuleOf, svc.FilesOf)
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

// ExecRunner is the production ToolRunner: it runs the bundled helper scripts as
// subprocesses. It is best-effort and not exercised by the offline test suite
// (which uses RecordedRunner); it exists so a real run works where node/php and
// the analyzers are installed.
type ExecRunner struct {
	// HelperDir holds the bundled helper scripts (ts.mjs, php.php).
	HelperDir string
	// RepoRoot is where subprocesses run.
	RepoRoot string
}

// helperRuntime maps a logical helper to its runtime binary and script.
func (r *ExecRunner) helperRuntime(name string) (bin, script, installHint string, ok bool) {
	switch name {
	case "typescript":
		return "node", filepath.Join(r.HelperDir, "ts.mjs"),
			"install Node.js and run `npm i -g dependency-cruiser` (+ ts-morph in the helper)", true
	case "php":
		return "php", filepath.Join(r.HelperDir, "php.php"),
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
	if _, err := exec.LookPath(bin); err != nil {
		return nil, &plane.ToolMissingError{Tool: bin, Hint: hint}
	}
	full := append([]string{script}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Dir = r.RepoRoot
	cmd.Stdin = strings.NewReader(string(stdin))
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if ok := asExit(err, &ee); ok && ee.ExitCode() == toolMissingExit {
			return nil, &plane.ToolMissingError{Tool: name, Hint: hint}
		}
		return nil, fmt.Errorf("derive: helper %q failed: %w", name, err)
	}
	return out, nil
}

// Version returns the resolved version of a runtime; helpers embed analyzer
// versions in their report, so this is only used for the runtime itself.
func (r *ExecRunner) Version(ctx context.Context, name string) (string, error) {
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
