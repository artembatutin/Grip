package testrigor

import (
	"fmt"

	"github.com/artembatutin/grip/internal/plane"
)

// Intent is the test-rigor plane's parsed, validated view of one module's
// grip.yaml `testRigor:` section. It is opaque to the engine; only this plane's
// Reconcile reads it. Unlike the architecture plane (where an absent section is
// treated as an empty-but-strict one), test-rigor is opt-in per module: a module
// with no testRigor section makes no rigor claims and contributes no violations.
type Intent struct {
	ModuleID string
	// RequiredBehaviors are the behaviors the human declares must be meaningfully
	// tested — the small boundary declaration this plane governs.
	RequiredBehaviors []string
	// MutationThreshold is the minimum mutation score (0..100) the module commits
	// to. Threshold-tamper fires when it is lowered vs baseline.
	MutationThreshold int
	// HasThreshold distinguishes "declared 0" from "not declared" so a module that
	// never set a threshold is not treated as having lowered one.
	HasThreshold bool
	// BoundaryContract records that the module claims a verified boundary contract
	// (a mutation-checked test of its public surface).
	BoundaryContract bool
	// HasSection records whether the module declared a testRigor section at all.
	HasSection bool
}

// rawSection mirrors the YAML under a module's `testRigor:` key. Strict decoding
// (unknown fields rejected) is enforced by the manifest loader (KnownFields), so
// a typo inside a governed section fails closed — same guarantee the architecture
// plane relies on.
type rawSection struct {
	RequiredBehaviors []string `yaml:"requiredBehaviors"`
	MutationThreshold *int     `yaml:"mutationThreshold"`
	BoundaryContract  bool     `yaml:"boundaryContract"`
}

// parseIntent decodes and validates one module's testRigor section. An absent
// section yields a zero Intent with HasSection=false (opt-out, no claims). A
// malformed section or an out-of-range threshold is a fail-closed error.
func parseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (Intent, error) {
	intent := Intent{ModuleID: mod.ID}
	if !raw.Present {
		return intent, nil
	}
	intent.HasSection = true
	var sec rawSection
	if err := raw.Decode(&sec); err != nil {
		return Intent{}, err
	}
	for _, b := range sec.RequiredBehaviors {
		if b == "" {
			return Intent{}, fmt.Errorf("module %s: testRigor.requiredBehaviors contains an empty entry", mod.ID)
		}
	}
	intent.RequiredBehaviors = sec.RequiredBehaviors
	intent.BoundaryContract = sec.BoundaryContract
	if sec.MutationThreshold != nil {
		t := *sec.MutationThreshold
		if t < 0 || t > 100 {
			return Intent{}, fmt.Errorf("module %s: testRigor.mutationThreshold %d out of range (want 0..100)", mod.ID, t)
		}
		intent.MutationThreshold = t
		intent.HasThreshold = true
	}
	return intent, nil
}
