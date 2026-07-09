package behavior

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

func mustDerive(t *testing.T, root string, runner plane.ToolRunner) *Model {
	t.Helper()
	m, err := (&Plane{}).derive(context.Background(), refsFor(root, "src/checkout"), deriveSvc(root, runner))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return m
}

func boundary(t *testing.T, m *Model, module, name string) *BoundaryState {
	t.Helper()
	st := m.Module(module)
	if st == nil {
		t.Fatalf("module %s not in model", module)
	}
	b := st.Boundary(name)
	if b == nil {
		t.Fatalf("boundary %s not in module %s", name, module)
	}
	return b
}

func writePin(t *testing.T, root, module, name, content string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(module), ".grip", "behavior")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".snap"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureTS is a behavior-typescript report with one boundary/one case.
func captureTS(caseOutput string) string {
	return `{"tool":{"name":"grip-behavior-ts","version":"1"},"modules":[
	  {"module":"src/checkout","boundaries":[
	    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"cases":[
	      {"name":"happy","output":"` + caseOutput + `"}]}]}]}`
}

// TestDeriveRoundTripNormalizes is the core property: a snapshot pinned from one
// run matches a later run that changed only run-to-run noise (a timestamp), and
// stops matching when the observable output changes for real. This is what makes
// a behavior-preserving rewrite pass and an output-changing rewrite block.
func TestDeriveRoundTripNormalizes(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"src/checkout/index.ts": "export const placeOrder = () => 'ok'\n",
	})
	r := newStubRunner()

	// First capture (timestamp T0). No pin yet → observed, unpinned.
	r.reports[toolTypeScript] = []byte(captureTS("order ok at 2024-01-02T03:04:05Z"))
	m := mustDerive(t, root, r)
	b := boundary(t, m, "src/checkout", "placeOrder")
	if !b.Observed || b.Reduced || b.Pinned || b.DerivedDigest == "" {
		t.Fatalf("unexpected first-capture state: %+v", b)
	}

	// Pin exactly what Derive produced (as `grip ratify behavior` would).
	writePin(t, root, "src/checkout", "placeOrder", b.DerivedSnapshot)

	// Behavior-preserving change: only the timestamp differs → digests MATCH.
	r.reports[toolTypeScript] = []byte(captureTS("order ok at 2025-06-06T06:06:06Z"))
	b = boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder")
	if !b.Pinned || b.DerivedDigest != b.PinnedDigest {
		t.Fatalf("behavior-preserving change should match pin: derived=%s pinned=%s", b.DerivedDigest, b.PinnedDigest)
	}

	// Observable change: the output text changed → digests DIFFER (drift).
	r.reports[toolTypeScript] = []byte(captureTS("order REJECTED at 2025-06-06T06:06:06Z"))
	b = boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder")
	if b.DerivedDigest == b.PinnedDigest {
		t.Fatal("observable change should not match the pin")
	}
}

// TestDeriveReduced confirms a helper-flagged nondeterministic boundary degrades
// to reduced (no derived digest to trust), never a silent pin.
func TestDeriveReduced(t *testing.T) {
	root := writeRepo(t, map[string]string{"src/checkout/index.ts": "x\n"})
	r := newStubRunner()
	r.reports[toolTypeScript] = []byte(`{"tool":{"name":"ts","version":"1"},"modules":[
	  {"module":"src/checkout","boundaries":[
	    {"name":"placeOrder","file":"src/checkout/index.ts","line":1,"nondeterministic":true,"cases":[]}]}]}`)
	b := boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder")
	if !b.Observed || !b.Reduced || b.DerivedDigest != "" {
		t.Fatalf("expected reduced with no digest: %+v", b)
	}
}

// TestDeriveBaseline confirms the baseline tool populates BaseDigest (for the
// intentional-render path) and that its absence is benign.
func TestDeriveBaseline(t *testing.T) {
	root := writeRepo(t, map[string]string{"src/checkout/index.ts": "x\n"})
	r := newStubRunner()
	r.reports[toolTypeScript] = []byte(captureTS("ok"))
	baseSnap := canonicalSnapshot("src/checkout", "placeOrder", []Sample{{"happy", "old"}})
	r.reports[baselineTool] = []byte(`{"modules":[{"module":"src/checkout","boundaries":[
	  {"name":"placeOrder","snapshot":` + jsonString(baseSnap) + `}]}]}`)

	b := boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder")
	if !b.BasePresent || b.BaseDigest != digest(baseSnap) {
		t.Fatalf("baseline not folded in: %+v", b)
	}

	// Without a baseline report, BasePresent is false (benign).
	delete(r.reports, baselineTool)
	b = boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder")
	if b.BasePresent {
		t.Fatal("absent baseline must not set BasePresent")
	}
}

// TestDeriveFailClosed confirms a missing capture helper is a fail-closed derive
// error (the gate turns it into a block), never a silent empty pass.
func TestDeriveFailClosed(t *testing.T) {
	root := writeRepo(t, map[string]string{"src/checkout/index.ts": "x\n"})
	r := newStubRunner()
	r.missing[toolTypeScript] = "install the behavior capture helper"
	_, err := (&Plane{}).derive(context.Background(), refsFor(root, "src/checkout"), deriveSvc(root, r))
	if err == nil {
		t.Fatal("expected fail-closed error on missing capture helper")
	}
	var tm *plane.ToolMissingError
	if !errors.As(err, &tm) {
		t.Fatalf("expected ToolMissingError, got %v", err)
	}
}

// TestDeriveDeterministic confirms two derives of identical input yield an
// identical derived digest (byte-stability, NFR-1).
func TestDeriveDeterministic(t *testing.T) {
	root := writeRepo(t, map[string]string{"src/checkout/index.ts": "x\n"})
	r := newStubRunner()
	r.reports[toolTypeScript] = []byte(captureTS("ok at 2024-01-02T03:04:05Z"))
	first := boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder").DerivedDigest
	for i := 0; i < 10; i++ {
		if got := boundary(t, mustDerive(t, root, r), "src/checkout", "placeOrder").DerivedDigest; got != first {
			t.Fatalf("run %d digest drift: %s != %s", i, got, first)
		}
	}
}

// jsonString renders a Go string as a JSON string literal (for embedding a
// canonical snapshot, which contains newlines, into a baseline fixture).
func jsonString(s string) string {
	var b []byte
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		default:
			b = append(b, string(r)...)
		}
	}
	b = append(b, '"')
	return string(b)
}
