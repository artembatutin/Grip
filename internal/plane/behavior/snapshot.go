package behavior

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// snapshotVersion tags the canonical snapshot format. It is part of the pinned
// bytes, so changing normalization (a new stripper) changes every digest — a
// deliberate, reviewed re-pin rather than silent drift.
const snapshotVersion = "grip:behavior/v1"

// Sample is one observed case at a boundary: a named case and the observed
// output as the capture helper recorded it (before normalization).
type Sample struct {
	Case   string
	Output string
}

// normalizers strip run-to-run noise so a boundary whose behavior is identical
// up to timestamps/ordering/addresses produces a byte-identical snapshot, while a
// boundary that changes MEANINGFULLY does not (and Reconcile blocks on the
// delta). This is the plane's answer to the hard problem (plan/05 §"cheap, stable
// capture"): normalize hard, and report what will not normalize as reduced
// confidence rather than pinning noise. Applied in order; the earlier, more
// specific patterns (timestamps, uuids) run before the greedy hex matchers.
var normalizers = []struct {
	re   *regexp.Regexp
	repl string
}{
	// ISO-8601 timestamps: 2024-01-02T03:04:05(.123)?(Z|±01:00).
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[Zz]|[+-]\d{2}:\d{2})`), "<ts>"},
	// UUIDs.
	{regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`), "<uuid>"},
	// Hex pointers / addresses: 0xdeadbeef.
	{regexp.MustCompile(`0x[0-9a-fA-F]+`), "<addr>"},
	// Long hex runs: content hashes (git sha, md5, sha256) captured in output.
	{regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`), "<hash>"},
}

// normalizeLine strips volatile substrings from one observed output line and
// trims trailing whitespace. It is pure and deterministic.
func normalizeLine(s string) string {
	for _, n := range normalizers {
		s = n.re.ReplaceAllString(s, n.repl)
	}
	return strings.TrimRight(s, " \t")
}

// canonicalSnapshot renders a boundary's normalized behavior as the exact bytes
// that get pinned to <module>/.grip/behavior/<boundary>.snap. Cases are sorted
// (the "strip ordering" normalization: the capture helper may emit them in any
// order) and each output is normalized and newline-escaped so one case is one
// line. Because Derive reproduces this text from the same capture and `grip
// ratify behavior` writes exactly this text, a clean run is byte-identical — zero
// drift by construction.
func canonicalSnapshot(moduleID, boundary string, samples []Sample) string {
	sorted := append([]Sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Case != sorted[j].Case {
			return sorted[i].Case < sorted[j].Case
		}
		return sorted[i].Output < sorted[j].Output
	})
	var b strings.Builder
	b.WriteString(snapshotVersion + "\n")
	fmt.Fprintf(&b, "module: %s\n", moduleID)
	fmt.Fprintf(&b, "boundary: %s\n", boundary)
	b.WriteString("---\n")
	for _, s := range sorted {
		out := strings.ReplaceAll(s.Output, "\r\n", "\n")
		out = strings.ReplaceAll(out, "\n", `\n`)
		fmt.Fprintf(&b, "- %s: %s\n", s.Case, out)
	}
	return b.String()
}

// digest is the content address of a snapshot (of derived text or of the pinned
// file's bytes). Reconcile compares digests, never raw text, so messages stay
// short and comparison is cheap; equal digest ⇔ equal snapshot.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
