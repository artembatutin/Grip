package behavior

import (
	"strings"
	"testing"
)

// TestNormalizeStripsNoise proves the normalizer actually strips run-to-run noise
// — the whole reason a behavior-preserving rewrite passes despite changed
// timestamps/addresses. Each pair differs only in volatile content and MUST
// normalize to the same line.
func TestNormalizeStripsNoise(t *testing.T) {
	pairs := []struct{ a, b, wantContains string }{
		{"order placed at 2024-01-02T03:04:05Z", "order placed at 2025-11-30T23:59:59.500+01:00", "<ts>"},
		{"id=0x7ffe1234", "id=0xdeadbeef", "<addr>"},
		{"trace 550e8400-e29b-41d4-a716-446655440000", "trace 550e8400-e29b-41d4-a716-999999999999", "<uuid>"},
		{"etag d41d8cd98f00b204e9800998ecf8427e", "etag a94a8fe5ccb19ba61c4c0873d391e987", "<hash>"},
	}
	for _, p := range pairs {
		na, nb := normalizeLine(p.a), normalizeLine(p.b)
		if na != nb {
			t.Errorf("normalize(%q)=%q != normalize(%q)=%q", p.a, na, p.b, nb)
		}
		if !strings.Contains(na, p.wantContains) {
			t.Errorf("normalize(%q)=%q missing %q", p.a, na, p.wantContains)
		}
	}
}

// TestNormalizeKeepsMeaning ensures the normalizer does NOT erase real behavioral
// differences — a genuine output change must survive as a different line.
func TestNormalizeKeepsMeaning(t *testing.T) {
	if normalizeLine("status: ok") == normalizeLine("status: error") {
		t.Fatal("normalizer erased a meaningful difference")
	}
	if got := normalizeLine("plain unchanging text"); got != "plain unchanging text" {
		t.Fatalf("normalizer altered stable text: %q", got)
	}
}

// TestCanonicalSnapshotOrderStable proves case ordering is normalized away: the
// same cases in any order produce byte-identical snapshots (and thus digests).
func TestCanonicalSnapshotOrderStable(t *testing.T) {
	forward := canonicalSnapshot("m", "b", []Sample{{"a", "1"}, {"b", "2"}, {"c", "3"}})
	shuffled := canonicalSnapshot("m", "b", []Sample{{"c", "3"}, {"a", "1"}, {"b", "2"}})
	if forward != shuffled {
		t.Fatalf("order not normalized:\n%s\n---\n%s", forward, shuffled)
	}
	if digest(forward) != digest(shuffled) {
		t.Fatal("digests differ despite identical canonical text")
	}
}

// TestCanonicalSnapshotFormat pins the exact serialized form (it is what gets
// committed to disk and reviewed) and confirms newline escaping keeps one case
// per line.
func TestCanonicalSnapshotFormat(t *testing.T) {
	got := canonicalSnapshot("src/checkout", "placeOrder", []Sample{
		{"happy", "order <addr> ok"},
		{"multi", "line1\nline2"},
	})
	want := "grip:behavior/v1\n" +
		"module: src/checkout\n" +
		"boundary: placeOrder\n" +
		"---\n" +
		"- happy: order <addr> ok\n" +
		"- multi: line1\\nline2\n"
	if got != want {
		t.Fatalf("canonical snapshot mismatch\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

// TestDigestDistinguishes confirms meaningfully different snapshots hash apart
// (the drift signal the gate relies on).
func TestDigestDistinguishes(t *testing.T) {
	a := canonicalSnapshot("m", "b", []Sample{{"c", "ok"}})
	b := canonicalSnapshot("m", "b", []Sample{{"c", "changed"}})
	if digest(a) == digest(b) {
		t.Fatal("different behavior produced the same digest")
	}
}
