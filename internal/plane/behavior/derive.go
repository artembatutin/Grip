package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
)

// Logical tool names the ToolRunner resolves. They are DISTINCT from the
// architecture ("typescript"/"php") and test-rigor ("testrigor-*") tool names so
// the three planes' recorded reports never collide in one .grip-analysis dir. In
// production these map to bundled helpers that piggyback on the module's existing
// test run and record observed boundary I/O; in tests the RecordedRunner replays
// <name>.json.
const (
	toolTypeScript = "behavior-typescript"
	toolPHP        = "behavior-php"
	toolGo         = "behavior-go"
	// baselineTool yields the boundary snapshots as of the git baseline (HEAD /
	// base branch), used ONLY to render a re-pin as an intentional change. Its
	// absence is benign (nil → no intentional rendering), NOT a fail-closed block:
	// the gate decision compares reality against the working-tree pin and never
	// needs the baseline. In a real deployment the CI action populates it from git
	// history (git show HEAD:<snap>), mirroring the analyzer-report seam.
	baselineTool = "behavior-baseline"
	// snapshotDir is the per-module directory holding git-tracked pins.
	snapshotDir = ".grip/behavior"
	snapshotExt = ".snap"
)

// behaviorReport is the normalized output of a language's capture helper: per
// module, the boundaries it observed and, for each, the cases (name → output)
// captured from real runs. Its shape mirrors what a facade-level recorder
// naturally produces, so the helper is thin and Grip owns normalization/scoring.
type behaviorReport struct {
	Tool    analyzerInfo    `json:"tool"`
	Modules []moduleCapture `json:"modules"`
}

type analyzerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type moduleCapture struct {
	Module     string            `json:"module"`
	Boundaries []boundaryCapture `json:"boundaries"`
}

type boundaryCapture struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
	// Nondeterministic is the helper's own flakiness verdict (e.g. it ran the
	// boundary's tests more than once and the normalized output varied). Such a
	// boundary is reported as reduced confidence, never silently pinned (NFR-9).
	Nondeterministic bool          `json:"nondeterministic"`
	Cases            []caseCapture `json:"cases"`
}

type caseCapture struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

// baselineReport is the prior-commit pin snapshot for the intentional-render
// path. It carries the canonical snapshot text per boundary; the plane digests it
// the same way as a pinned file so a re-pin (base text ≠ current pin) is detected.
type baselineReport struct {
	Modules []baselineModule `json:"modules"`
}

type baselineModule struct {
	Module     string             `json:"module"`
	Boundaries []baselineBoundary `json:"boundaries"`
}

type baselineBoundary struct {
	Name     string `json:"name"`
	Snapshot string `json:"snapshot"`
}

// capturedBoundary is the plane's normalized view of one observed boundary.
type capturedBoundary struct {
	File     string
	Line     int
	Reduced  bool
	Snapshot string // canonical normalized text (empty when reduced)
}

// derive is the plane's I/O body: capture reality through the language helpers
// (recorded in tests), read the git-tracked pins from each module directory, and
// fetch the baseline — folding all three into the non-graph Model so Reconcile
// stays pure. Any missing capture helper or malformed report is fail-closed.
func (p *Plane) derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (*Model, error) {
	captured, err := p.capture(ctx, mods, svc)
	if err != nil {
		return nil, err // fail-closed: tool-missing or malformed capture
	}
	base := p.baseline(ctx, svc)

	model := &Model{}
	for _, mod := range mods {
		st := &ModuleState{ModuleID: mod.ID, Language: mod.Language}
		byName := map[string]*BoundaryState{}

		if capMod, ok := captured[mod.ID]; ok {
			st.Captured = true
			names := make([]string, 0, len(capMod))
			for name := range capMod {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				cb := capMod[name]
				bs := &BoundaryState{Name: name, File: cb.File, Line: cb.Line, Observed: true}
				if cb.Reduced {
					bs.Reduced = true
				} else {
					bs.DerivedSnapshot = cb.Snapshot
					bs.DerivedDigest = digest(cb.Snapshot)
				}
				byName[name] = bs
			}
		}

		for name, content := range readPins(mod.Path) {
			bs := byName[name]
			if bs == nil {
				bs = &BoundaryState{Name: name}
				byName[name] = bs
			}
			bs.Pinned = true
			bs.PinnedDigest = digest(string(content))
		}

		if base != nil {
			for name, d := range base[mod.ID] {
				bs := byName[name]
				if bs == nil {
					bs = &BoundaryState{Name: name}
					byName[name] = bs
				}
				bs.BasePresent = true
				bs.BaseDigest = d
			}
		}

		for _, bs := range byName {
			st.Boundaries = append(st.Boundaries, bs)
		}
		model.Modules = append(model.Modules, st)
	}

	model.index()
	return model, nil
}

// capture runs each language's capture helper and normalizes the reports into
// per-module, per-boundary state. A governed language with no capture helper, or
// a helper that errors (missing tool), is fail-closed (never a silent skip).
func (p *Plane) capture(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]map[string]capturedBoundary, error) {
	byLang := map[string][]plane.ModuleRef{}
	for _, m := range mods {
		byLang[m.Language] = append(byLang[m.Language], m)
	}
	langs := make([]string, 0, len(byLang))
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	out := map[string]map[string]capturedBoundary{}
	for _, lang := range langs {
		if lang == "go" {
			captured, err := captureGo(ctx, byLang[lang], svc)
			if err != nil {
				return nil, err
			}
			for moduleID, bounds := range captured {
				out[moduleID] = bounds
			}
			continue
		}
		name, ok := toolName(lang)
		if !ok {
			return nil, fmt.Errorf("behavior: no capture helper for language %q (has %d governed modules)", lang, len(byLang[lang]))
		}
		raw, err := svc.Tools.Run(ctx, name, helperArgs(lang, svc), nil)
		if err != nil {
			return nil, err // fail-closed: tool-missing or capture error
		}
		var rep behaviorReport
		if err := json.Unmarshal(raw, &rep); err != nil {
			return nil, fmt.Errorf("behavior: parse %s capture report: %w", lang, err)
		}
		for _, mr := range rep.Modules {
			bounds := out[mr.Module]
			if bounds == nil {
				bounds = map[string]capturedBoundary{}
				out[mr.Module] = bounds
			}
			for _, br := range mr.Boundaries {
				bounds[br.Name] = normalizeBoundary(mr.Module, br)
			}
		}
	}
	return out, nil
}

// normalizeBoundary folds one raw boundary capture into normalized state. A
// helper-flagged nondeterministic boundary is reduced (never pinned); otherwise
// its cases are normalized into a stable canonical snapshot.
func normalizeBoundary(moduleID string, br boundaryCapture) capturedBoundary {
	cb := capturedBoundary{File: br.File, Line: br.Line}
	if br.Nondeterministic {
		cb.Reduced = true
		return cb
	}
	samples := make([]Sample, 0, len(br.Cases))
	for _, c := range br.Cases {
		samples = append(samples, Sample{Case: c.Name, Output: normalizeLine(c.Output)})
	}
	cb.Snapshot = canonicalSnapshot(moduleID, br.Name, samples)
	return cb
}

// baseline fetches the prior-commit pin snapshots. ANY failure (helper unknown in
// production, file absent in tests, decode error, empty) yields nil — no baseline,
// so the intentional-render path simply does not fire. This is never a
// fail-closed condition: the gate decision does not depend on the baseline.
func (p *Plane) baseline(ctx context.Context, svc plane.DeriveServices) map[string]map[string]string {
	out, err := svc.Tools.Run(ctx, baselineTool, []string{"--repo-root", svc.RepoRoot}, nil)
	if err != nil {
		return nil
	}
	var br baselineReport
	if json.Unmarshal(out, &br) != nil || len(br.Modules) == 0 {
		return nil
	}
	m := make(map[string]map[string]string, len(br.Modules))
	for _, bm := range br.Modules {
		bb := make(map[string]string, len(bm.Boundaries))
		for _, b := range bm.Boundaries {
			bb[b.Name] = digest(b.Snapshot)
		}
		m[bm.Module] = bb
	}
	return m
}

// readPins reads a module's git-tracked snapshot files under
// <moduleDir>/.grip/behavior/*.snap. A missing directory or unreadable entry
// simply contributes no pin (the boundary is then treated as unpinned). The
// snapshot file's bytes ARE the pinned baseline — the plane never keeps pins in
// engine state, which is what makes drift impossible by construction.
func readPins(moduleDir string) map[string][]byte {
	if moduleDir == "" {
		return nil
	}
	dir := filepath.Join(moduleDir, filepath.FromSlash(snapshotDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), snapshotExt) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(e.Name(), snapshotExt)] = b
	}
	return out
}

// toolName maps a language to its behavior capture helper (M0/M1 support PHP+TS).
func toolName(lang string) (string, bool) {
	switch lang {
	case "typescript":
		return toolTypeScript, true
	case "php":
		return toolPHP, true
	case "go":
		return toolGo, true
	default:
		return "", false
	}
}

// helperArgs mirrors the other planes' argument convention: the repo root plus
// the language's roots.
func helperArgs(lang string, svc plane.DeriveServices) []string {
	args := []string{"--repo-root", svc.RepoRoot}
	for _, s := range svc.Languages {
		if s.Language == lang {
			for _, r := range s.Roots {
				args = append(args, "--root", r)
			}
		}
	}
	return args
}
