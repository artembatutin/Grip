package contract

import (
	"fmt"
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// Kind ids — the three wire boundaries this plane governs. They double as the
// manifest sub-keys under `contract:` and as tokens in the rule ids and baseline
// filenames. Adding a kind is a closed, deliberate change (new checker + new
// policy rows), never open-ended.
const (
	KindAPI    = "api"
	KindEvents = "events"
	KindDB     = "db"
)

// kindsInOrder is the canonical, stable order kinds are processed and reported
// in (determinism, NFR-1). Reconcile and Rules both iterate this.
var kindsInOrder = []string{KindAPI, KindEvents, KindDB}

// Compat is a per-kind compatibility policy: which direction of change is allowed
// without a re-ratification. It is the knob that makes the reconcile a genuine
// policy application rather than a fixed differ.
type Compat string

const (
	// CompatBackward: a new producer must still serve OLD consumers. Removing or
	// tightening an in-use element breaks; adding optional elements is safe. The
	// common default for a public API.
	CompatBackward Compat = "backward"
	// CompatForward: an OLD producer must still serve NEW consumers. Adding a
	// required input is safe (old servers ignore it); removing an element still
	// breaks.
	CompatForward Compat = "forward"
	// CompatFull: both directions must hold — the strictest policy; any removal,
	// rename, tightening, or newly-required element breaks.
	CompatFull Compat = "full"
)

// KindIntent is one governed kind's declared policy.
type KindIntent struct {
	Kind   string
	Compat Compat
}

// Intent is the contract plane's parsed, validated view of one module's grip.yaml
// `contract:` section. It is opaque to the engine; only this plane's Reconcile
// reads it. Like test-rigor and behavior (and unlike architecture, which treats an
// absent section as empty-but-strict), the contract plane is opt-in per module: a
// module with no `contract:` section makes no claims and is never gated.
type Intent struct {
	ModuleID string
	// Kinds holds the governed kinds keyed by kind id, each with its compat policy.
	// A kind absent here is not governed for this module.
	Kinds map[string]KindIntent
	// HasSection records whether a `contract:` section was declared at all.
	HasSection bool
}

// Governs reports whether the module governs the given contract kind.
func (in Intent) Governs(kind string) (KindIntent, bool) {
	ki, ok := in.Kinds[kind]
	return ki, ok
}

// GovernedKinds returns the governed kind ids in canonical order.
func (in Intent) GovernedKinds() []string {
	var out []string
	for _, k := range kindsInOrder {
		if _, ok := in.Kinds[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// rawSection mirrors the YAML under a module's `contract:` key. Using explicit
// per-kind fields (rather than a map) means the strict manifest decoder
// (KnownFields) rejects an unknown kind key (e.g. `grpc:`) AND an unknown field
// inside a kind (e.g. a typo in `compat`) — fail-closed on any surprise, the same
// guarantee every plane relies on. A kind is governed iff its mapping is present
// (a non-nil pointer): `api: {}` or `api: { compat: backward }` governs it; a bare
// `api:` (null) does not.
type rawSection struct {
	API    *rawKind `yaml:"api"`
	Events *rawKind `yaml:"events"`
	DB     *rawKind `yaml:"db"`
}

// rawKind is one kind's raw mapping. An empty mapping defaults compat to backward.
type rawKind struct {
	Compat string `yaml:"compat"`
}

// parseIntent decodes and validates one module's contract section. An absent
// section yields a zero Intent with HasSection=false (opt-out, no claims). A
// malformed section, an unknown compatibility policy, or a `contract:` section
// that declares no kind at all is a fail-closed error.
func parseIntent(raw plane.ManifestSection, mod plane.ModuleRef) (Intent, error) {
	in := Intent{ModuleID: mod.ID, Kinds: map[string]KindIntent{}}
	if !raw.Present {
		return in, nil
	}
	in.HasSection = true
	var sec rawSection
	if err := raw.Decode(&sec); err != nil {
		return Intent{}, err
	}
	for _, kd := range []struct {
		id  string
		raw *rawKind
	}{
		{KindAPI, sec.API},
		{KindEvents, sec.Events},
		{KindDB, sec.DB},
	} {
		if kd.raw == nil {
			continue // kind not governed
		}
		compat, err := parseCompat(mod.ID, kd.id, kd.raw.Compat)
		if err != nil {
			return Intent{}, err
		}
		in.Kinds[kd.id] = KindIntent{Kind: kd.id, Compat: compat}
	}
	if len(in.Kinds) == 0 {
		// A `contract:` section that governs nothing is almost certainly an
		// authoring mistake; fail closed rather than silently governing nothing.
		return Intent{}, fmt.Errorf("module %s: contract section declares no governed kind (add one of: %s)", mod.ID, joinKinds())
	}
	return in, nil
}

// parseCompat validates a compat policy string, defaulting an empty value to
// backward (the common case). Any other value is fail-closed.
func parseCompat(moduleID, kind, raw string) (Compat, error) {
	switch Compat(raw) {
	case "":
		return CompatBackward, nil // documented default
	case CompatBackward:
		return CompatBackward, nil
	case CompatForward:
		return CompatForward, nil
	case CompatFull:
		return CompatFull, nil
	default:
		return "", fmt.Errorf("module %s: contract.%s.compat %q is not a valid policy (allowed: backward, forward, full)", moduleID, kind, raw)
	}
}

func joinKinds() string {
	ks := append([]string(nil), kindsInOrder...)
	sort.Strings(ks)
	out := ""
	for i, k := range ks {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}
