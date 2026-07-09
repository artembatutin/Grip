package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadSampleDoc(t *testing.T) Document {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "sample-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the viewer must consume the JSON report unchanged: %v", err)
	}
	return doc
}

// TestHTMLRendersGraphAndDiffFromJSONFixture proves the exit criterion for Part B:
// the viewer renders the derived graph (with the allowed-vs-actual overlay and
// violation highlighting) and the shape diff, driven purely from a JSON report
// fixture — no gate run, no source of truth of its own.
func TestHTMLRendersGraphAndDiffFromJSONFixture(t *testing.T) {
	out := HTML(loadSampleDoc(t))

	// The graph: an <svg> with a node per module and an edge per dependency.
	must(t, out, "<svg")
	for _, id := range []string{"src/application", "src/domain", "src/infrastructure"} {
		must(t, out, id)
	}
	must(t, out, "<line") // at least one edge drawn

	// allowed-vs-actual overlay: application→domain is allowed, infrastructure→
	// domain is drift (not declared). Both edge styles must be present.
	must(t, out, "edge-ok")
	must(t, out, "edge-drift")

	// violation highlighting by tier: a blocking node, an advisory node, a
	// judgment node — colored by tier, never by rule id.
	must(t, out, "tier-a")
	must(t, out, "tier-b")
	must(t, out, "tier-c")

	// the facade overlay flags infrastructure's derived-reachable "Secret" that is
	// beyond its declared facade.
	must(t, out, "beyond facade")

	// the shape diff.
	must(t, out, "Shape diff")
	must(t, out, "edge src/infrastructure")
	must(t, out, "facade edited on purpose")

	// findings are grouped and the judgment tier is clearly marked non-blocking.
	must(t, out, "never gates")
}

// TestHTMLHasNoEditingAffordance is the hard guardrail for M4 Part B (PRD §4.3,
// ACP §11.1): the read-only viewer must offer NO affordance to change a manifest,
// edge, or facade — and no way to reach a server or execute script. Any of these
// tokens appearing is rejected on sight. There is no studio.
func TestHTMLHasNoEditingAffordance(t *testing.T) {
	out := strings.ToLower(HTML(loadSampleDoc(t)))
	banned := []string{
		"<form", "<input", "<textarea", "<button", "<select", "contenteditable",
		"<script", "javascript:", "fetch(", "xmlhttprequest", "websocket", "eventsource",
		"onclick", "oninput", "onchange", "onsubmit", "onload", " action=", "formaction",
	}
	for _, tok := range banned {
		if strings.Contains(out, tok) {
			t.Errorf("read-only viewer contains a forbidden affordance %q — it must not let a user edit, submit, or fetch", tok)
		}
	}
}

// TestHTMLIsSelfContainedAndDeterministic proves the viewer is a static, offline,
// pure rendering: no external resources (no network authority), and byte-identical
// output for the same Document.
func TestHTMLIsSelfContainedAndDeterministic(t *testing.T) {
	doc := loadSampleDoc(t)
	out := HTML(doc)
	lower := strings.ToLower(out)
	// Note: the SVG arrowheads use url(#fragment) — an internal same-document
	// reference, not an external resource — so we target genuinely external forms.
	for _, tok := range []string{"http://", "https://", "src=", "<link", "//cdn", "@import", "url(http", "url(//", "url('", "url(\""} {
		if strings.Contains(lower, tok) {
			t.Errorf("viewer references an external resource %q — it must be fully self-contained", tok)
		}
	}
	if HTML(doc) != out {
		t.Error("HTML rendering is not deterministic")
	}
}

// TestHTMLHandlesEmptyReport proves the viewer degrades gracefully with no graph
// and no delta (e.g. a plane that derives no IR).
func TestHTMLHandlesEmptyReport(t *testing.T) {
	out := HTML(Document{Decision: "pass", ExitCode: 0, PlanesRun: []string{"test-rigor"}})
	must(t, out, "No derived graph")
	must(t, out, "No shape delta")
	must(t, out, "PASS")
}

func must(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected rendered HTML to contain %q", needle)
	}
}
