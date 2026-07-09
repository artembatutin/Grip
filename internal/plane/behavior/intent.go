package behavior

import (
	"fmt"
	"regexp"

	"github.com/artembatutin/grip/internal/plane"
)

// Intent is the behavior plane's parsed, validated view of one module's
// grip.yaml `behavior:` section. It is opaque to the engine; only this plane's
// Reconcile reads it. Like test-rigor (and unlike architecture, which treats an
// absent section as empty-but-strict), the behavior plane is opt-in per module:
// a module with no `behavior:` section makes no claims and is never gated.
//
// The defining ergonomic of this plane is that the human declares NOTHING about
// the behavior itself — they ratify deltas. The `pin` list only NAMES which
// boundaries to gate; the pinned baseline is the git-tracked snapshot file, never
// anything the human writes by hand.
type Intent struct {
	ModuleID string
	// Pin names the boundaries the human wants gated. The snapshot FILE under
	// <module>/.grip/behavior/<name>.snap is the pinned baseline; this list marks
	// which boundaries `grip ratify behavior` captures and which unpinned
	// boundaries are advised (Tier B). Gating itself follows the files, so a
	// boundary already backed by a snapshot is gated whether or not it is listed.
	Pin []string
	// HasSection records whether a `behavior:` section was declared at all.
	HasSection bool
}

// rawSection mirrors the YAML under a module's `behavior:` key. Strict decoding
// (unknown fields rejected) is enforced by the manifest loader (KnownFields), so
// a typo inside a governed section fails closed — the same guarantee every plane
// relies on.
type rawSection struct {
	Pin []string `yaml:"pin"`
}

// boundaryName constrains a pin entry to a filesystem-safe identifier: the name
// becomes a snapshot file name (<name>.snap), so path separators, "..", and other
// surprises are rejected up front (fail-closed) rather than sanitized silently. It
// must start with a letter or digit (excluding "." and ".." and hidden names).
var boundaryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// parseIntent decodes and validates one module's behavior section. An absent
// section yields a zero Intent with HasSection=false (opt-out, no claims). A
// malformed section, an empty pin entry, a duplicate, or an unsafe boundary name
// is a fail-closed error.
func parseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (Intent, error) {
	in := Intent{ModuleID: mod.ID}
	if !raw.Present {
		return in, nil
	}
	in.HasSection = true
	var sec rawSection
	if err := raw.Decode(&sec); err != nil {
		return Intent{}, err
	}
	seen := map[string]bool{}
	for _, b := range sec.Pin {
		if b == "" {
			return Intent{}, fmt.Errorf("module %s: behavior.pin contains an empty entry", mod.ID)
		}
		if !boundaryName.MatchString(b) {
			return Intent{}, fmt.Errorf("module %s: behavior.pin entry %q is not a valid boundary name (allowed: letters, digits, . _ -)", mod.ID, b)
		}
		if seen[b] {
			return Intent{}, fmt.Errorf("module %s: behavior.pin lists %q twice", mod.ID, b)
		}
		seen[b] = true
		in.Pin = append(in.Pin, b)
	}
	return in, nil
}
