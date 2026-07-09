package acceptance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
)

// TestDeterminismIRHash asserts the merged PHP+TS IR for the base fixture hashes
// byte-identically across repeated derivations — the value the determinism CI
// matrix asserts (NFR-1). Run many times to catch any map-iteration-order leak.
func TestDeterminismIRHash(t *testing.T) {
	fx := fixturesDir(t)
	base := filepath.Join(fx, "base")
	reg := cli.BuildRegistry()
	cfg, err := config.Load(base, reg)
	if err != nil {
		t.Fatal(err)
	}
	run := func() (*gate.Outcome, error) {
		return gate.Run(context.Background(), cfg, reg, gate.Options{
			CI:     true,
			Tools:  &derive.RecordedRunner{AnalysisDir: filepath.Join(base, ".grip-analysis")},
			Commit: "fixed",
		})
	}
	first, err := run()
	if err != nil {
		t.Fatal(err)
	}
	if first.IRHash == "" {
		t.Fatal("expected a non-empty IR hash")
	}
	for i := 0; i < 100; i++ {
		out, err := run()
		if err != nil {
			t.Fatal(err)
		}
		if out.IRHash != first.IRHash {
			t.Fatalf("run %d: IR hash %s != %s", i, out.IRHash, first.IRHash)
		}
		if out.Decision != first.Decision {
			t.Fatalf("run %d: decision drift", i)
		}
	}
}

// TestMergedIRIsMultiLanguage asserts both PHP and TS modules land in one IR
// with no engine language-branching (D2): the base graph must contain modules of
// both languages.
func TestMergedIRIsMultiLanguage(t *testing.T) {
	fx := fixturesDir(t)
	base := filepath.Join(fx, "base")
	reg := cli.BuildRegistry()
	cfg, err := config.Load(base, reg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI: true, Tools: &derive.RecordedRunner{AnalysisDir: filepath.Join(base, ".grip-analysis")}, Commit: "fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	langs := map[string]int{}
	for _, m := range out.Graph.Modules {
		langs[m.Language]++
	}
	if langs["typescript"] == 0 || langs["php"] == 0 {
		t.Fatalf("merged IR is not multi-language: %v", langs)
	}
}
