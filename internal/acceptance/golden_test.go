package acceptance

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden report files")

// TestGoldenReports pins the full human report for representative scenarios to
// golden files (plan/08 §4). Because reports carry only repo-relative paths and
// module ids — no temp dirs, no timestamps — the golden output is stable, and a
// changed user-facing string is a visible, reviewed diff.
func TestGoldenReports(t *testing.T) {
	cases := []scenario{
		{name: "clean-base", wantDecision: "pass"},
		{name: "illegal-dependency", overlay: "illegal-dependency", wantDecision: "block"},
		{name: "reduced-confidence", overlay: "reduced-confidence", wantDecision: "block"},
		{name: "php-illegal-dependency", overlay: "php-illegal-dependency", wantDecision: "block"},
	}
	goldenDir := filepath.Join(fixturesDir(t), "..", "golden")
	for _, sc := range cases {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			_, human := runScenario(t, sc)
			goldenPath := filepath.Join(goldenDir, sc.name+".report.txt")
			if *update {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, []byte(human), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if human != string(want) {
				t.Errorf("report mismatch for %s.\n--- got ---\n%s\n--- want ---\n%s", sc.name, human, string(want))
			}
		})
	}
}
