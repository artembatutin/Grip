package testrigor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// Logical tool names the ToolRunner resolves. They are DISTINCT from the
// architecture plane's "typescript"/"php" so the two planes' recorded reports
// never collide in one .grip-analysis dir. In production these map to bundled
// helpers wrapping Stryker (TS/JS) and Infection (PHP) plus the test runner for
// inventory/coverage; in tests the RecordedRunner replays <name>.json.
const (
	toolTypeScript = "testrigor-typescript"
	toolPHP        = "testrigor-php"
	// baselineTool yields the prior-commit required-test set and thresholds. Its
	// absence is benign (no baseline to compare against), NOT a fail-closed block:
	// a missing mutation tool blocks, but a missing baseline just disables the
	// comparison rules. In a real deployment the CI action populates it from git
	// history (internal/vcs), mirroring how analyzer reports come from real tools.
	baselineTool = "testrigor-baseline"
)

// toolReport is the normalized output of a language's test-rigor helper: mutation
// results, coverage, mock ratio, and a per-test inventory, keyed by module. Its
// shape mirrors what Stryker/Infection + the runner naturally produce, so the
// helper is thin and Grip owns the scoring (quarantine, aggregation).
type toolReport struct {
	Tool         analyzerInfo   `json:"tool"`
	CoverageTool analyzerInfo   `json:"coverageTool"`
	Modules      []moduleReport `json:"modules"`
}

type analyzerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type moduleReport struct {
	Module    string       `json:"module"`
	Coverage  int          `json:"coverage"`
	MockRatio int          `json:"mockRatio"`
	Tests     []testReport `json:"tests"`
}

type testReport struct {
	ID        string   `json:"id"`
	Behaviors []string `json:"behaviors"`
	Contract  bool     `json:"contract"`
	Skipped   bool     `json:"skipped"`
	Only      bool     `json:"only"`
	Flaky     bool     `json:"flaky"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	// MutantsInScope / MutantsKilled are this test's slice of the module's mutants
	// (Stryker/Infection attribute mutants to the tests that cover them).
	MutantsInScope int `json:"mutantsInScope"`
	MutantsKilled  int `json:"mutantsKilled"`
}

// baselineReport is the prior-commit snapshot for the comparison rules.
type baselineReport struct {
	Modules []baselineModuleReport `json:"modules"`
}

type baselineModuleReport struct {
	Module        string              `json:"module"`
	Threshold     *int                `json:"threshold"`
	MutationScore *int                `json:"mutationScore"`
	MockRatio     *int                `json:"mockRatio"`
	RequiredTests map[string][]string `json:"requiredTests"`
}

// derive is the plane's I/O body: fetch each language's report through the
// injected ToolRunner (recorded in tests), quarantine flaky tests, aggregate into
// the non-graph Model, all behind a content-hash cache so unchanged modules are
// not re-mutated. Any missing mutation tool or malformed report is fail-closed.
func (p *Plane) derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (*Model, error) {
	newCache := p.newCache
	if newCache == nil {
		newCache = newFSCache
	}
	cache := newCache(svc.RepoRoot)
	changed, haveChanged := changedModules(ctx, svc.RepoRoot, svc.ModuleOf)

	byLang := map[string][]plane.ModuleRef{}
	for _, m := range mods {
		byLang[m.Language] = append(byLang[m.Language], m)
	}
	langs := make([]string, 0, len(byLang))
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	model := &Model{}
	for _, lang := range langs {
		name, ok := toolName(lang)
		if !ok {
			// A language with governed modules but no test-rigor helper is a
			// fail-closed error, never a silent skip (NFR-6, mirrors the deriver).
			return nil, fmt.Errorf("test-rigor: no mutation helper for language %q (has %d governed modules)", lang, len(byLang[lang]))
		}
		refs := append([]plane.ModuleRef(nil), byLang[lang]...)
		sort.Slice(refs, func(a, b int) bool { return refs[a].ID < refs[b].ID })

		// Cheap version probe (no analyzer run) so the cache key captures a tool
		// upgrade without paying for a mutation run.
		ver, _ := svc.Tools.Version(ctx, name)

		type slot struct {
			ref    plane.ModuleRef
			key    string
			cached *ModuleState
		}
		slots := make([]slot, 0, len(refs))
		coldCount := 0
		for _, r := range refs {
			key := contentHash(svc.RepoRoot, r.ID, svc.FilesOf(r.ID), name, ver)
			var cs *ModuleState
			forceFresh := haveChanged && changed[r.ID]
			if !forceFresh {
				if st, ok := cache.Get(key); ok {
					cs = st
				}
			}
			if cs == nil {
				coldCount++
			}
			slots = append(slots, slot{ref: r, key: key, cached: cs})
		}

		// Run the (expensive) mutation helper only when at least one module is
		// cold — a fully warm cache skips it entirely (changed-only locally / full
		// on a cold CI checkout, from one path with no mode flag).
		repByID := map[string]moduleReport{}
		if coldCount > 0 {
			out, err := svc.Tools.Run(ctx, name, helperArgs(lang, svc), nil)
			if err != nil {
				return nil, err // fail-closed: tool-missing or analyzer error
			}
			var rep toolReport
			if err := json.Unmarshal(out, &rep); err != nil {
				return nil, fmt.Errorf("test-rigor: parse %s report: %w", lang, err)
			}
			for _, mr := range rep.Modules {
				repByID[mr.Module] = mr
			}
		}

		for _, s := range slots {
			if s.cached != nil {
				model.Modules = append(model.Modules, s.cached)
				continue
			}
			mr, analyzed := repByID[s.ref.ID]
			st := buildModuleState(s.ref, mr, analyzed)
			cache.Put(s.key, st)
			model.Modules = append(model.Modules, st)
		}
	}

	model.Baseline = p.baseline(ctx, svc)
	model.index()
	return model, nil
}

// buildModuleState folds one module's raw report into derived state, QUARANTINING
// flaky tests before any aggregate is computed: a flaky test contributes to the
// inventory (so Reconcile can fail closed on it) but never to the mutation score
// or the contract kill counts, so a non-deterministic signal cannot inflate
// results or silently pass.
func buildModuleState(ref plane.ModuleRef, mr moduleReport, analyzed bool) *ModuleState {
	st := &ModuleState{ModuleID: ref.ID, Language: ref.Language, Analyzed: analyzed}
	if !analyzed {
		return st
	}
	st.Coverage = clampPct(mr.Coverage)
	st.MockRatio = clampPct(mr.MockRatio)

	// A `.only` anywhere silently shadows every non-only sibling (jest/mocha).
	hasOnly := false
	for _, t := range mr.Tests {
		if t.Only {
			hasOnly = true
			break
		}
	}

	killed, inScope := 0, 0
	anyContract, allContractFlaky := false, true
	for _, t := range mr.Tests {
		ts := TestState{
			ID:        t.ID,
			Behaviors: append([]string(nil), t.Behaviors...),
			Contract:  t.Contract,
			Flaky:     t.Flaky,
			File:      t.File,
			Line:      t.Line,
			Skipped:   t.Skipped || (hasOnly && !t.Only),
		}
		st.Tests = append(st.Tests, ts)

		if t.Contract {
			anyContract = true
			if !t.Flaky {
				allContractFlaky = false
			}
		}
		if t.Flaky {
			continue // quarantined from all scoring
		}
		killed += t.MutantsKilled
		inScope += t.MutantsInScope
		if t.Contract {
			st.ContractMutants += t.MutantsInScope
			st.ContractKilled += t.MutantsKilled
			if st.ContractTestID == "" {
				st.ContractTestID = t.ID
				st.ContractFile = t.File
				st.ContractLine = t.Line
			}
		}
	}

	st.ContractPresent = anyContract
	st.ContractFlaky = anyContract && allContractFlaky
	if st.ContractFlaky {
		// No trustworthy contract test: record a representative flaky one so the
		// cannot-verify message can name it.
		for _, t := range mr.Tests {
			if t.Contract {
				st.ContractTestID = t.ID
				st.ContractFile = t.File
				st.ContractLine = t.Line
				break
			}
		}
	}
	st.MutationScore = pct(killed, inScope)
	return st
}

// baseline fetches the prior-commit snapshot. ANY failure (helper unknown in
// production, file absent in tests, decode error) yields nil — no baseline, so
// the comparison rules simply do not fire. This is not a fail-closed condition:
// you cannot tamper against a baseline that does not exist.
func (p *Plane) baseline(ctx context.Context, svc plane.DeriveServices) map[string]*BaselineState {
	out, err := svc.Tools.Run(ctx, baselineTool, []string{"--repo-root", svc.RepoRoot}, nil)
	if err != nil {
		return nil
	}
	var br baselineReport
	if json.Unmarshal(out, &br) != nil || len(br.Modules) == 0 {
		return nil
	}
	m := make(map[string]*BaselineState, len(br.Modules))
	for _, bm := range br.Modules {
		bs := &BaselineState{RequiredTests: map[string][]string{}}
		if bm.Threshold != nil {
			bs.Threshold, bs.HasThreshold = clampPct(*bm.Threshold), true
		}
		if bm.MutationScore != nil {
			bs.MutationScore, bs.HasScore = clampPct(*bm.MutationScore), true
		}
		if bm.MockRatio != nil {
			bs.MockRatio, bs.HasMockRatio = clampPct(*bm.MockRatio), true
		}
		for beh, ids := range bm.RequiredTests {
			bs.RequiredTests[beh] = append([]string(nil), ids...)
		}
		m[bm.Module] = bs
	}
	return m
}

// toolName maps a language to its test-rigor helper name (M0 supports PHP + TS).
func toolName(lang string) (string, bool) {
	switch lang {
	case "typescript":
		return toolTypeScript, true
	case "php":
		return toolPHP, true
	default:
		return "", false
	}
}

// helperArgs mirrors the architecture deriver's argument convention: the repo
// root plus the language's roots.
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

// pct returns killed/total as a rounded 0..100 integer (round-half-up, integer
// math so there is no float non-determinism). total==0 yields 0.
func pct(killed, total int) int {
	if total <= 0 {
		return 0
	}
	return clampPct((killed*100 + total/2) / total)
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
