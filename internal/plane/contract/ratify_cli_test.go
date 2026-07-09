package contract_test

// Exercises the real `grip ratify contract <module>` command through cli.App — the
// harness's adopt() helper writes baselines via BaselinesFor directly, so this is
// the only coverage of the actual CLI wiring: the adopt-current-as-baseline edit,
// and the block→pass transition it produces at the gate (a governed kind with no
// ratified baseline fails closed until adopted).

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	app := &cli.App{Stdout: &out, Stderr: &errb, Reg: cli.BuildRegistry()}
	code := app.Run(args)
	return code, out.String(), errb.String()
}

func baselinePath(root, module, kind string) string {
	return filepath.Join(root, filepath.FromSlash(module), ".grip", "contract", kind+".contract")
}

func TestRatifyContractCommand(t *testing.T) {
	root := newRepo(t, nil) // no baselines adopted yet
	t.Chdir(root)
	adir := analysisDir(root)

	// Before any ratification, every governed kind has no baseline → the gate fails
	// closed (never a false pass on an un-adopted wire contract).
	if out, human := runGate(t, root); out.ExitCode != 2 {
		t.Fatalf("gate before ratify should fail closed (exit 2), got %d\n%s", out.ExitCode, human)
	}

	// Adopt checkout's api contract via the real command.
	code, stdout, stderr := runCLI(t, "ratify", "contract", "--analysis-dir", adir, "src/checkout")
	if code != 0 {
		t.Fatalf("ratify exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	got, err := os.ReadFile(baselinePath(root, "src/checkout", "api"))
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
	if string(got) != "openapi: checkout v1" {
		t.Fatalf("ratify wrote unexpected baseline content: %q", string(got))
	}

	// Adopt orders' events + db contracts too, so all governed kinds are ratified.
	if code, _, se := runCLI(t, "ratify", "contract", "--analysis-dir", adir, "app/Orders"); code != 0 {
		t.Fatalf("ratify orders exit %d: %s", code, se)
	}
	for _, kind := range []string{"events", "db"} {
		if _, err := os.Stat(baselinePath(root, "app/Orders", kind)); err != nil {
			t.Fatalf("orders %s baseline not written: %v", kind, err)
		}
	}

	// Now the gate passes: every governed kind is resolved with a ratified baseline.
	if out, human := runGate(t, root); out.ExitCode != 0 {
		t.Fatalf("gate after ratify should pass, got %d\n%s", out.ExitCode, human)
	}

	// An agent removes an in-use API field → the gate blocks pending re-ratification.
	writeFiles(t, root, map[string]string{".grip-analysis/contract-api-typescript.json": apiRemoved})
	if out, human := runGate(t, root); out.ExitCode != 1 {
		t.Fatalf("gate should block on a removed field, got %d\n%s", out.ExitCode, human)
	}
}

func TestRatifyContractRefusesUnderivable(t *testing.T) {
	// A checker that yields no current shape cannot be adopted (fail-closed): the
	// command refuses and exits 2, writing nothing.
	root := newRepo(t, map[string]string{
		".grip-analysis/contract-api-typescript.json": `{"tool":{"name":"t","version":"1"},"modules":[
		  {"module":"src/checkout","resolved":true,"currentShape":"","changes":[]}]}`,
	})
	t.Chdir(root)
	code, stdout, stderr := runCLI(t, "ratify", "contract", "--analysis-dir", analysisDir(root), "src/checkout")
	if code != 2 {
		t.Fatalf("expected fail-closed exit 2, got %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(baselinePath(root, "src/checkout", "api")); !os.IsNotExist(err) {
		t.Fatal("an underivable contract must not be adopted")
	}
}

func TestRatifyContractUnknownModule(t *testing.T) {
	root := newRepo(t, nil)
	t.Chdir(root)
	code, _, stderr := runCLI(t, "ratify", "contract", "--analysis-dir", analysisDir(root), "src/nope")
	if code != 3 { // usage error
		t.Fatalf("expected usage exit 3, got %d (%s)", code, stderr)
	}
}
