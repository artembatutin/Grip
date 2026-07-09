// Package acceptance is the end-to-end M0 gate (plan/03 M0.11, plan/08): scripted
// "agent" diffs over the synthetic PHP+TS fixture repo run through the real gate,
// asserting the exact decision, exit code, and report. It is hermetic and
// offline — derivers consume recorded analyzer reports (plan/08 §1) — so it also
// doubles as the determinism proof.
package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/config"
	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/report"
)

func renderHuman(out *gate.Outcome) string {
	return report.Human(report.View{Outcome: out})
}

// fixturesDir returns testdata/fixtures relative to this source file.
func fixturesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures")
}

// scenario describes one fixture run: a base repo overlaid with a named scenario.
type scenario struct {
	name         string
	overlay      string            // scenarios/<overlay> dir applied over base (may be "")
	missingTools map[string]string // force a tool-missing fail-closed
	wantDecision string            // "pass" | "block"
	wantExit     int
	wantRule     string   // a rule id expected among violations ("" = none required)
	wantContains []string // substrings expected in the human report
	wantNotRule  string   // a rule id that must NOT appear ("" = no constraint)
}

// runScenario materializes base+overlay in a temp dir and runs the gate.
func runScenario(t *testing.T, sc scenario) (*gate.Outcome, string) {
	t.Helper()
	fx := fixturesDir(t)
	root := t.TempDir()
	copyTree(t, filepath.Join(fx, "base"), root)
	if sc.overlay != "" {
		applyOverlay(t, filepath.Join(fx, "scenarios", sc.overlay), root)
	}

	reg := cli.BuildRegistry()
	cfg, err := config.Load(root, reg)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	var tools = &derive.RecordedRunner{
		AnalysisDir: filepath.Join(root, ".grip-analysis"),
		Missing:     sc.missingTools,
	}
	out, err := gate.Run(context.Background(), cfg, reg, gate.Options{
		CI:     true,
		Tools:  tools,
		Commit: "test-commit",
	})
	if err != nil {
		// A usage error is only expected for malformed-manifest scenarios, which
		// assert on it separately; surface it here.
		t.Fatalf("gate run returned usage error: %v", err)
	}
	return out, renderHuman(out)
}

// copyTree recursively copies src into dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy tree: %v", err)
	}
}

// applyOverlay copies overlay files over root and honors a .delete manifest (one
// repo-relative path per line) so a scenario can also remove a file (e.g. delete
// a governed module's manifest).
func applyOverlay(t *testing.T, overlay, root string) {
	t.Helper()
	err := filepath.Walk(overlay, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(overlay, p)
		if err != nil {
			return err
		}
		if rel == ".delete" {
			return nil // handled below
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("apply overlay: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(overlay, ".delete")); err == nil {
		for _, line := range splitNonEmptyLines(string(b)) {
			_ = os.Remove(filepath.Join(root, filepath.FromSlash(line)))
		}
	}
}

func splitNonEmptyLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			if t := trimSpace(cur); t != "" {
				out = append(out, t)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if t := trimSpace(cur); t != "" {
		out = append(out, t)
	}
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
