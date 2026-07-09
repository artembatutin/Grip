package architecture

import (
	"fmt"

	"github.com/artembatutin/grip/internal/plane"
)

// Cycle policy values.
const (
	cyclesForbid        = "forbid"
	cyclesAllowInternal = "allow-internal"
)

// Intent is the architecture plane's parsed, validated view of one module's
// grip.yaml. It is opaque to the engine; only this plane's Reconcile reads it.
type Intent struct {
	ModuleID string
	// Facade is the declared public surface. Everything else is internal.
	Facade []string
	// Allow is the allowed outbound dependency set: module ids or layer names.
	// Absence is prohibition.
	Allow []string
	// Layer is the module's layer for direction rules (optional).
	Layer string
	// Cycles is the module's cycle policy: forbid | allow-internal.
	Cycles string
	// HasSection records whether the module declared an architecture section.
	HasSection bool
}

// rawSection mirrors the YAML under a module's `architecture:` key. Strict
// decoding (unknown fields rejected) is enforced by the manifest loader, so a
// typo inside a governed section fails closed.
type rawSection struct {
	Facade       []string `yaml:"facade"`
	Dependencies rawDeps  `yaml:"dependencies"`
	Cycles       string   `yaml:"cycles"`
}

type rawDeps struct {
	Allow []string `yaml:"allow"`
	Layer string   `yaml:"layer"`
}

// parseIntent decodes and validates one module's architecture section.
//
// A governed module with no architecture section is treated as declaring an
// empty one (facade: [], allow: [], cycles: forbid): it stays strict — real
// cross-module edges or an external reach still produce violations, nudging the
// author to declare a boundary — rather than being silently exempt. `grip init`
// generates the section so this never bites onboarding.
func parseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (Intent, error) {
	intent := Intent{ModuleID: mod.ID, Cycles: cyclesForbid}
	if !raw.Present {
		return intent, nil
	}
	intent.HasSection = true
	var sec rawSection
	if err := raw.Decode(&sec); err != nil {
		return Intent{}, err
	}
	intent.Facade = sec.Facade
	intent.Allow = sec.Dependencies.Allow
	intent.Layer = sec.Dependencies.Layer
	if sec.Cycles != "" {
		intent.Cycles = sec.Cycles
	}
	switch intent.Cycles {
	case cyclesForbid, cyclesAllowInternal:
	default:
		return Intent{}, fmt.Errorf("module %s: invalid cycles policy %q (want %q or %q)", mod.ID, intent.Cycles, cyclesForbid, cyclesAllowInternal)
	}
	return intent, nil
}
