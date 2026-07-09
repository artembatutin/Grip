package testrigor

import (
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

// section wraps a YAML fragment as a strict manifest section, exactly as the
// manifest loader hands one to a plane (KnownFields → unknown keys rejected).
func section(t *testing.T, yamlBody string) plane.ManifestSection {
	t.Helper()
	return plane.ManifestSection{
		Present: true,
		Decode: func(v interface{}) error {
			dec := yamlStrict(strings.NewReader(yamlBody))
			return dec.Decode(v)
		},
	}
}

func TestParseIntent(t *testing.T) {
	t.Run("absent section = opt-out", func(t *testing.T) {
		in, err := parseIntent(plane.ManifestSection{Present: false}, plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if in.HasSection {
			t.Fatal("absent section must not be HasSection")
		}
	})

	t.Run("valid full section", func(t *testing.T) {
		in, err := parseIntent(section(t, "requiredBehaviors: [checkout, refund]\nmutationThreshold: 80\nboundaryContract: true\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if !in.HasSection || !in.BoundaryContract || !in.HasThreshold || in.MutationThreshold != 80 {
			t.Fatalf("bad parse: %+v", in)
		}
		if len(in.RequiredBehaviors) != 2 {
			t.Fatalf("behaviors: %v", in.RequiredBehaviors)
		}
	})

	t.Run("unknown field is fail-closed (strict decode)", func(t *testing.T) {
		_, err := parseIntent(section(t, "mutationThreshold: 80\nmuationThreshold: 50\n"), plane.ModuleRef{ID: "m"})
		if err == nil {
			t.Fatal("expected strict-decode error on unknown field")
		}
	})

	t.Run("threshold out of range is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "mutationThreshold: 140\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected out-of-range error")
		}
		if _, err := parseIntent(section(t, "mutationThreshold: -5\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected out-of-range error")
		}
	})

	t.Run("empty behavior entry is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "requiredBehaviors: [\"\"]\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected empty-behavior error")
		}
	})

	t.Run("threshold 0 declared is distinct from absent", func(t *testing.T) {
		in, err := parseIntent(section(t, "mutationThreshold: 0\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if !in.HasThreshold || in.MutationThreshold != 0 {
			t.Fatalf("declared 0 must set HasThreshold: %+v", in)
		}
	})
}
