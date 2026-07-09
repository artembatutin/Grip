package behavior

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
			return yamlStrict(strings.NewReader(yamlBody)).Decode(v)
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

	t.Run("valid pin list", func(t *testing.T) {
		in, err := parseIntent(section(t, "pin: [placeOrder, total]\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if !in.HasSection || len(in.Pin) != 2 || in.Pin[0] != "placeOrder" || in.Pin[1] != "total" {
			t.Fatalf("bad parse: %+v", in)
		}
	})

	t.Run("empty section is opt-in with no pins", func(t *testing.T) {
		in, err := parseIntent(section(t, "pin: []\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if !in.HasSection || len(in.Pin) != 0 {
			t.Fatalf("empty pin: %+v", in)
		}
	})

	t.Run("unknown field is fail-closed (strict decode)", func(t *testing.T) {
		if _, err := parseIntent(section(t, "pin: [x]\npn: [y]\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected strict-decode error on unknown field")
		}
	})

	t.Run("empty pin entry is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "pin: [\"\"]\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected empty-entry error")
		}
	})

	t.Run("unsafe boundary name is fail-closed", func(t *testing.T) {
		for _, bad := range []string{"a/b", "has space", "..", "a:b"} {
			if _, err := parseIntent(section(t, "pin: [\""+bad+"\"]\n"), plane.ModuleRef{ID: "m"}); err == nil {
				t.Fatalf("expected invalid-name error for %q", bad)
			}
		}
	})

	t.Run("duplicate pin entry is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "pin: [x, x]\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected duplicate error")
		}
	})
}
