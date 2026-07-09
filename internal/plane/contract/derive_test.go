package contract

import (
	"context"
	"errors"
	"testing"

	"github.com/artembatutin/grip/internal/plane"
)

const apiRemovedReport = `{"tool":{"name":"oasdiff","version":"1"},"modules":[
  {"module":"src/checkout","resolved":true,"currentShape":"openapi:v2","changes":[
    {"nature":"removed","element":"GET /orders#total","consumer":"billing","file":"src/checkout/openapi.yaml","line":42}]}]}`

func deriveModel(t *testing.T, root string, runner plane.ToolRunner, ids ...string) *Model {
	t.Helper()
	d, err := (&Plane{}).derive(context.Background(), refsFor(root, ids...), deriveSvc(root, runner))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return d
}

func TestDeriveResolvedChange(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v1")
	runner := newStubRunner().with(toolAPITypeScript, apiRemovedReport)

	m := deriveModel(t, root, runner, "src/checkout")
	ks := m.Module("src/checkout").Kind(KindAPI)
	if ks == nil {
		t.Fatal("expected api kind state")
	}
	if !ks.BaselinePresent || !ks.HasVerdict || !ks.CheckerResolved {
		t.Fatalf("flags: baseline=%v verdict=%v resolved=%v", ks.BaselinePresent, ks.HasVerdict, ks.CheckerResolved)
	}
	if ks.CurrentShape != "openapi:v2" {
		t.Fatalf("currentShape = %q", ks.CurrentShape)
	}
	if len(ks.Changes) != 1 || ks.Changes[0].Nature != NatureRemoved || ks.Changes[0].Consumer != "billing" {
		t.Fatalf("changes: %+v", ks.Changes)
	}
	if ks.Changes[0].File != "src/checkout/openapi.yaml" || ks.Changes[0].Line != 42 {
		t.Fatalf("location lost: %+v", ks.Changes[0])
	}
}

func TestDeriveToolMissingFailClosed(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v1")
	runner := newStubRunner().miss(toolAPITypeScript, "install the api contract checker")

	_, err := (&Plane{}).derive(context.Background(), refsFor(root, "src/checkout"), deriveSvc(root, runner))
	if err == nil {
		t.Fatal("expected fail-closed derive error")
	}
	var tm *plane.ToolMissingError
	if !errors.As(err, &tm) {
		t.Fatalf("expected ToolMissingError, got %v", err)
	}
}

func TestDeriveNoBaselineFile(t *testing.T) {
	// The checker returns a verdict, but no ratified baseline exists on disk: the
	// filesystem is authoritative → BaselinePresent must be false (Reconcile then
	// fails closed with "no baseline").
	root := writeRepo(t, nil)
	runner := newStubRunner().with(toolAPITypeScript, apiRemovedReport)

	ks := deriveModel(t, root, runner, "src/checkout").Module("src/checkout").Kind(KindAPI)
	if ks == nil || ks.BaselinePresent {
		t.Fatalf("expected BaselinePresent=false, got %+v", ks)
	}
	if !ks.HasVerdict {
		t.Fatal("expected the checker verdict to be recorded")
	}
}

func TestDeriveUnresolvedChecker(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "app/Refund", KindDB, "schema:v1")
	report := `{"tool":{"name":"migra","version":"1"},"modules":[
	  {"module":"app/Refund","resolved":false,"reason":"prior migration state unreadable"}]}`
	runner := newStubRunner().with(toolDB, report)

	ks := deriveModel(t, root, runner, "app/Refund").Module("app/Refund").Kind(KindDB)
	if ks == nil || ks.CheckerResolved {
		t.Fatalf("expected CheckerResolved=false, got %+v", ks)
	}
	if ks.CheckerReason != "prior migration state unreadable" {
		t.Fatalf("reason = %q", ks.CheckerReason)
	}
}

func TestDeriveRepinnedFromPrior(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v2") // current baseline on disk
	report := `{"tool":{"name":"oasdiff","version":"1"},"modules":[
	  {"module":"src/checkout","resolved":true,"currentShape":"openapi:v2","changes":[]}]}`
	// The prior commit's baseline differs → the human re-ratified.
	baseline := `{"modules":[{"module":"src/checkout","kinds":[{"kind":"api","baseline":"openapi:v1"}]}]}`
	runner := newStubRunner().with(toolAPITypeScript, report).with(baselineTool, baseline)

	ks := deriveModel(t, root, runner, "src/checkout").Module("src/checkout").Kind(KindAPI)
	if ks == nil || !ks.Repinned {
		t.Fatalf("expected Repinned=true, got %+v", ks)
	}
}

func TestDeriveNotRepinnedWhenPriorMatches(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v2")
	report := `{"tool":{"name":"oasdiff","version":"1"},"modules":[
	  {"module":"src/checkout","resolved":true,"currentShape":"openapi:v2","changes":[]}]}`
	baseline := `{"modules":[{"module":"src/checkout","kinds":[{"kind":"api","baseline":"openapi:v2"}]}]}`
	runner := newStubRunner().with(toolAPITypeScript, report).with(baselineTool, baseline)

	ks := deriveModel(t, root, runner, "src/checkout").Module("src/checkout").Kind(KindAPI)
	if ks == nil || ks.Repinned {
		t.Fatalf("expected Repinned=false when prior matches, got %+v", ks)
	}
}

func TestDeriveBaselineAbsenceIsBenign(t *testing.T) {
	// No contract-baseline report at all → nil prior, no Repinned, no error.
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v2")
	report := `{"tool":{"name":"oasdiff","version":"1"},"modules":[
	  {"module":"src/checkout","resolved":true,"currentShape":"openapi:v2","changes":[]}]}`
	runner := newStubRunner().with(toolAPITypeScript, report)

	ks := deriveModel(t, root, runner, "src/checkout").Module("src/checkout").Kind(KindAPI)
	if ks == nil || ks.Repinned {
		t.Fatalf("baseline absence must be benign (no Repinned), got %+v", ks)
	}
}

func TestBaselinesForAdoptsCurrentShape(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v1")
	runner := newStubRunner().with(toolAPITypeScript, apiRemovedReport)
	m := deriveModel(t, root, runner, "src/checkout")

	files := BaselinesFor(m, "src/checkout", map[string]bool{KindAPI: true})
	if len(files) != 1 {
		t.Fatalf("expected 1 baseline file, got %+v", files)
	}
	f := files[0]
	if f.Kind != KindAPI || f.Missing || f.Content != "openapi:v2" {
		t.Fatalf("baseline file: %+v", f)
	}
	if f.Path != "src/checkout/.grip/contract/api.contract" {
		t.Fatalf("path = %q", f.Path)
	}
}

func TestBaselinesForRefusesUnderivableShape(t *testing.T) {
	// A verdict with an empty currentShape cannot be adopted (fail-closed): Missing.
	root := writeRepo(t, nil)
	report := `{"tool":{"name":"oasdiff","version":"1"},"modules":[
	  {"module":"src/checkout","resolved":true,"currentShape":"","changes":[]}]}`
	runner := newStubRunner().with(toolAPITypeScript, report)
	m := deriveModel(t, root, runner, "src/checkout")

	files := BaselinesFor(m, "src/checkout", map[string]bool{KindAPI: true})
	if len(files) != 1 || !files[0].Missing {
		t.Fatalf("expected 1 Missing baseline file, got %+v", files)
	}
}

func TestBaselinesForFilterExcludes(t *testing.T) {
	root := writeRepo(t, nil)
	writeBaseline(t, root, "src/checkout", KindAPI, "openapi:v1")
	runner := newStubRunner().with(toolAPITypeScript, apiRemovedReport)
	m := deriveModel(t, root, runner, "src/checkout")

	if files := BaselinesFor(m, "src/checkout", map[string]bool{KindEvents: true}); len(files) != 0 {
		t.Fatalf("filter must exclude non-listed kinds, got %+v", files)
	}
}
