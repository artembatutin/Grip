package behavior_test

// Exercises the real `grip ratify behavior <module>` command through cli.App —
// the harness's pin() helper writes snapshots via SnapshotsFor directly, so this
// is the only coverage of the actual CLI wiring: the ratification edit, and the
// block→pass transition it produces at the gate.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/plane/behavior"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	app := &cli.App{Stdout: &out, Stderr: &errb, Reg: cli.BuildRegistry()}
	code := app.Run(args)
	return code, out.String(), errb.String()
}

func snapPath(root, module, boundary string) string {
	return filepath.Join(root, filepath.FromSlash(module), ".grip", "behavior", boundary+".snap")
}

func TestRatifyBehaviorCommand(t *testing.T) {
	root := newRepo(t, nil) // no pins yet
	t.Chdir(root)
	adir := analysisDir(root)

	// Ratify checkout: writes the placeOrder snapshot from current reality.
	code, stdout, stderr := runCLI(t, "ratify", "behavior", "--analysis-dir", adir, "src/checkout")
	if code != 0 {
		t.Fatalf("ratify exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	snap := snapPath(root, "src/checkout", "placeOrder")
	got, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
	// The written bytes are exactly the plane's canonical snapshot (byte-for-byte
	// what a later Derive reproduces — zero drift by construction).
	want := behavior.SnapshotsFor(deriveBehavior(t, root), "src/checkout", nil)
	if len(want) == 0 || string(got) != want[0].Content {
		t.Fatalf("ratify wrote non-canonical content:\n%q", string(got))
	}

	// Pin refund too, so both governed modules are clean, then the gate passes.
	if code, _, se := runCLI(t, "ratify", "behavior", "--analysis-dir", adir, "app/Refund"); code != 0 {
		t.Fatalf("ratify refund exit %d: %s", code, se)
	}
	if out, human := runGate(t, root); out.ExitCode != 0 {
		t.Fatalf("gate after ratify should pass, got %d\n%s", out.ExitCode, human)
	}

	// An agent changes observable output → the gate blocks pending ratification.
	writeFiles(t, root, map[string]string{".grip-analysis/behavior-typescript.json": tsChanged})
	if out, human := runGate(t, root); out.ExitCode != 1 {
		t.Fatalf("gate should block on observable change, got %d\n%s", out.ExitCode, human)
	}

	// Re-ratify → the gate passes again (the pin now records the new behavior).
	if code, _, se := runCLI(t, "ratify", "behavior", "--analysis-dir", adir, "src/checkout"); code != 0 {
		t.Fatalf("re-ratify exit %d: %s", code, se)
	}
	if out, human := runGate(t, root); out.ExitCode != 0 {
		t.Fatalf("gate after re-ratify should pass, got %d\n%s", out.ExitCode, human)
	}
}

func TestRatifyBehaviorRefusesNondeterministic(t *testing.T) {
	root := newRepo(t, map[string]string{".grip-analysis/behavior-typescript.json": tsFlaky})
	t.Chdir(root)
	adir := analysisDir(root)

	code, stdout, stderr := runCLI(t, "ratify", "behavior", "--analysis-dir", adir, "src/checkout")
	if code != 2 { // fail-closed: never pin an unstable snapshot
		t.Fatalf("expected fail-closed exit 2, got %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(snapPath(root, "src/checkout", "placeOrder")); !os.IsNotExist(err) {
		t.Fatal("a nondeterministic boundary must not be pinned")
	}
}

func TestRatifyBehaviorUnknownModule(t *testing.T) {
	root := newRepo(t, nil)
	t.Chdir(root)
	code, _, stderr := runCLI(t, "ratify", "behavior", "--analysis-dir", analysisDir(root), "src/nope")
	if code != 3 { // usage error
		t.Fatalf("expected usage exit 3, got %d (%s)", code, stderr)
	}
}
