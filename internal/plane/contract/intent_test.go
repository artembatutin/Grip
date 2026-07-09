package contract

import (
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

func TestParseIntent(t *testing.T) {
	t.Run("absent section = opt-out", func(t *testing.T) {
		in, err := parseIntent(plane.ManifestSection{Present: false}, plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if in.HasSection {
			t.Fatal("absent section must not be HasSection")
		}
		if len(in.Kinds) != 0 {
			t.Fatalf("absent section must govern no kinds: %+v", in.Kinds)
		}
	})

	t.Run("full section parses all kinds with policies", func(t *testing.T) {
		in, err := parseIntent(section(t, "api: { compat: backward }\nevents: { compat: full }\ndb: { compat: forward }\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if !in.HasSection {
			t.Fatal("expected HasSection")
		}
		want := map[string]Compat{KindAPI: CompatBackward, KindEvents: CompatFull, KindDB: CompatForward}
		for k, wc := range want {
			ki, ok := in.Governs(k)
			if !ok {
				t.Fatalf("expected kind %q governed", k)
			}
			if ki.Compat != wc {
				t.Fatalf("kind %q compat = %q, want %q", k, ki.Compat, wc)
			}
		}
	})

	t.Run("empty kind mapping defaults compat to backward", func(t *testing.T) {
		in, err := parseIntent(section(t, "api: {}\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		ki, ok := in.Governs(KindAPI)
		if !ok || ki.Compat != CompatBackward {
			t.Fatalf("expected api governed with default backward, got %+v ok=%v", ki, ok)
		}
	})

	t.Run("bare null kind is not governed", func(t *testing.T) {
		// `api:` with a null value yields a nil pointer → not governed. Governing a
		// kind requires a mapping (documented). Pair it with a real kind so the
		// section is not empty.
		in, err := parseIntent(section(t, "api:\ndb: {}\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := in.Governs(KindAPI); ok {
			t.Fatal("bare null api: must not govern api")
		}
		if _, ok := in.Governs(KindDB); !ok {
			t.Fatal("db: {} must govern db")
		}
	})

	t.Run("unknown kind is fail-closed (strict decode)", func(t *testing.T) {
		if _, err := parseIntent(section(t, "api: {}\ngrpc: {}\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected strict-decode error on unknown kind")
		}
	})

	t.Run("unknown field inside a kind is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "api: { compatt: backward }\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected strict-decode error on unknown kind field")
		}
	})

	t.Run("invalid compat policy is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "api: { compat: sideways }\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected invalid-compat error")
		}
	})

	t.Run("section governing nothing is fail-closed", func(t *testing.T) {
		if _, err := parseIntent(section(t, "{}\n"), plane.ModuleRef{ID: "m"}); err == nil {
			t.Fatal("expected error: a contract section must govern at least one kind")
		}
	})

	t.Run("GovernedKinds is canonical order", func(t *testing.T) {
		in, err := parseIntent(section(t, "db: {}\napi: {}\nevents: {}\n"), plane.ModuleRef{ID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		got := in.GovernedKinds()
		want := []string{KindAPI, KindEvents, KindDB}
		if len(got) != len(want) {
			t.Fatalf("GovernedKinds = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("GovernedKinds = %v, want %v", got, want)
			}
		}
	})
}
