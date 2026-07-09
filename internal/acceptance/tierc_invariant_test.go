package acceptance

import (
	"testing"

	"github.com/artembatutin/grip/internal/cli"
	"github.com/artembatutin/grip/internal/plane"
)

// TestNoRegisteredRuleIsPromotableTierC is the binary-wide contract invariant: no
// rule in any registered plane may be both Tier C and Promotable. Tier C is
// judgment-assisted (the only place an LLM enters Grip) and must never gate a
// merge, so making it promotable would be a category error. gate.decide excludes
// Tier C structurally and config refuses to promote it; this test stops the
// mistake at its source — a plane's Rules() declaration — for every plane the
// shipped binary registers, not just the architecture plane.
func TestNoRegisteredRuleIsPromotableTierC(t *testing.T) {
	for _, p := range cli.BuildRegistry().All() {
		for _, r := range p.Rules() {
			if r.Tier == plane.TierC && r.Promotable {
				t.Errorf("plane %q rule %q is Tier C and Promotable — Tier C rules must never be promotable", p.ID(), r.ID)
			}
		}
	}
}
