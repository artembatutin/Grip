package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// Logical tool names the ToolRunner resolves. They are DISTINCT from the
// architecture ("typescript"/"php"), test-rigor ("testrigor-*"), and behavior
// ("behavior-*") tool names so the four planes' recorded reports never collide in
// one .grip-analysis dir. Each is a wrapped breaking-change CHECKER (NFR-8), not a
// bespoke differ: in production they map to bundled helpers running an OpenAPI-diff
// (api), a JSON-Schema/Protobuf/Avro compatibility checker (events), and a
// migration-compat linter (db); in tests the RecordedRunner replays <name>.json.
//
// The per-kind sub-derivers below express the plan's "heterogeneous sub-derivers
// per contract kind": api fans out per language (framework routes/schemas differ
// between PHP and TS), while events and db are language-neutral single tools.
const (
	toolAPITypeScript = "contract-api-typescript"
	toolAPIPHP        = "contract-api-php"
	toolAPIGo         = "contract-api-go"
	toolEvents        = "contract-events"
	toolDB            = "contract-db"
	// baselineTool yields the prior-commit ratified baseline per module/kind, used
	// ONLY to render a re-ratification as an intentional change. Its absence is
	// benign (nil → no intentional rendering), NOT a fail-closed block: the gate
	// decision compares the current shape against the working-tree baseline and
	// never needs the prior. In a real deployment the CI action populates it from
	// git history (internal/vcs: git show HEAD:<baseline>), mirroring the
	// analyzer-report seam.
	baselineTool = "contract-baseline"

	// baselineDir is the per-module directory holding git-tracked ratified
	// baselines; baselineExt is one baseline artifact's extension. The file IS the
	// declared contract (approval-test style), never engine state — zero drift by
	// construction, exactly as the behavior plane's .snap files.
	baselineDir = ".grip/contract"
	baselineExt = ".contract"
)

// checkerReport is the normalized output of one kind's wrapped checker: per
// module, whether it resolved a comparable contract, the classified changes it
// found (current vs the ratified baseline), and the canonical current shape (so
// ratify can adopt it). Its shape mirrors what an OpenAPI-diff / schema-compat /
// migration-lint tool naturally produces, so the helper is thin and Grip owns the
// policy judgment.
type checkerReport struct {
	Tool    analyzerInfo    `json:"tool"`
	Modules []moduleVerdict `json:"modules"`
}

type analyzerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type moduleVerdict struct {
	Module string `json:"module"`
	// Resolved is the checker's own verdict that it could compare current vs the
	// declared (and, where a rule needs it, prior) version. It DEFAULTS to false
	// (fail-closed): a checker that omits it is treated as unresolved.
	Resolved bool `json:"resolved"`
	// Reason explains an unresolved verdict (e.g. "prior migration state
	// unreadable"), surfaced in the cannot-verify report.
	Reason string `json:"reason"`
	// CurrentShape is the canonical current contract text `grip ratify contract`
	// writes as the new baseline.
	CurrentShape string      `json:"currentShape"`
	Changes      []changeRec `json:"changes"`
}

type changeRec struct {
	Nature   string `json:"nature"`
	Element  string `json:"element"`
	Consumer string `json:"consumer"`
	Detail   string `json:"detail"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// baselineReport is the prior-commit baseline for the intentional-render path. It
// carries each governed kind's prior baseline text; the plane digests it the same
// way as the working-tree baseline so a re-ratification (prior text ≠ current) is
// detected.
type baselineReport struct {
	Modules []baselineModule `json:"modules"`
}

type baselineModule struct {
	Module string          `json:"module"`
	Kinds  []baselineKindR `json:"kinds"`
}

type baselineKindR struct {
	Kind     string `json:"kind"`
	Baseline string `json:"baseline"`
}

// kindVerdict is the plane's internal view of one (module, kind) checker verdict.
type kindVerdict struct {
	Resolved     bool
	Reason       string
	CurrentShape string
	Changes      []Change
}

// subDeriver derives one contract kind's verdicts. The three implementations are
// deliberately heterogeneous — api fans out per language, events and db are single
// language-neutral tools — which is the point: the plane contract must carry
// per-kind sub-derivers, not one uniform shape.
type subDeriver interface {
	kind() string
	derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]*kindVerdict, error)
}

// derive is the plane's I/O body: run each kind's checker (recorded in tests), read
// the git-tracked ratified baselines from each module directory, and fetch the
// prior-commit baseline — folding all three points in time into the versioned
// Model so Reconcile stays pure. Any checker error (a missing tool for a kind that
// runs) is fail-closed.
func (p *Plane) derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (*Model, error) {
	subs := []subDeriver{apiDeriver{}, eventsDeriver{}, dbDeriver{}}
	verdicts := map[string]map[string]*kindVerdict{} // kind -> moduleID -> verdict
	for _, s := range subs {
		v, err := s.derive(ctx, mods, svc)
		if err != nil {
			return nil, err // fail-closed: checker tool-missing or malformed report
		}
		verdicts[s.kind()] = v
	}
	prior := p.baseline(ctx, svc)

	model := &Model{}
	for _, mod := range mods {
		st := &ModuleState{ModuleID: mod.ID, Language: mod.Language, Kinds: map[string]*KindState{}}
		baselines := readBaselines(mod.Path)
		for _, kind := range kindsInOrder {
			baseBytes, hasBaseline := baselines[kind]
			v := verdicts[kind][mod.ID]
			if !hasBaseline && v == nil {
				continue // nothing observed for this kind; Reconcile handles governance
			}
			ks := &KindState{Kind: kind, BaselinePresent: hasBaseline}
			if v != nil {
				ks.HasVerdict = true
				ks.CheckerResolved = v.Resolved
				ks.CheckerReason = v.Reason
				ks.CurrentShape = v.CurrentShape
				ks.Changes = v.Changes
			}
			if hasBaseline && prior != nil {
				if pd, ok := prior[mod.ID][kind]; ok && pd != digest(baseBytes) {
					ks.Repinned = true
				}
			}
			st.Kinds[kind] = ks
		}
		model.Modules = append(model.Modules, st)
	}
	model.index()
	return model, nil
}

// apiDeriver derives the api kind, fanning out per language: each governed
// language's api surface comes from that framework's route/schema definitions, so
// there is one checker per language.
type apiDeriver struct{}

func (apiDeriver) kind() string { return KindAPI }

func (apiDeriver) derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]*kindVerdict, error) {
	byLang := map[string][]plane.ModuleRef{}
	for _, m := range mods {
		byLang[m.Language] = append(byLang[m.Language], m)
	}
	langs := make([]string, 0, len(byLang))
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	out := map[string]*kindVerdict{}
	for _, lang := range langs {
		if lang == "go" {
			v, err := deriveGoAPI(byLang[lang], svc)
			if err != nil {
				return nil, err
			}
			for id, kv := range v {
				out[id] = kv
			}
			continue
		}
		tool, ok := apiToolName(lang)
		if !ok {
			// No api checker exists for this language. Do not hard-fail the whole
			// plane; a module of this language that actually governs api surfaces a
			// localized cannot-verify (no verdict) in Reconcile instead.
			continue
		}
		v, err := runChecker(ctx, svc, tool, helperArgs(lang, svc))
		if err != nil {
			return nil, err // fail-closed: an api checker that runs and errors blocks
		}
		for id, kv := range v {
			out[id] = kv
		}
	}
	return out, nil
}

// eventsDeriver derives the events kind with a single language-neutral checker
// (JSON Schema / Protobuf / Avro compatibility).
type eventsDeriver struct{}

func (eventsDeriver) kind() string { return KindEvents }

func (eventsDeriver) derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]*kindVerdict, error) {
	v, err := runChecker(ctx, svc, toolEvents, allRootsArgs(svc))
	if err != nil && !hasContractArtifacts(svc, []string{".proto", ".avsc", ".jsonschema"}) {
		return map[string]*kindVerdict{}, nil
	}
	return v, err
}

// dbDeriver derives the db kind with a single migration-compat checker: it parses
// the new migration files (those not in the prior commit) and flags destructive or
// incompatible ones.
type dbDeriver struct{}

func (dbDeriver) kind() string { return KindDB }

func (dbDeriver) derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (map[string]*kindVerdict, error) {
	v, err := runChecker(ctx, svc, toolDB, allRootsArgs(svc))
	if err != nil && !hasContractArtifacts(svc, []string{".sql"}) {
		return map[string]*kindVerdict{}, nil
	}
	return v, err
}

// runChecker runs one checker tool and normalizes its report into per-module
// verdicts. A missing tool or malformed report is fail-closed (propagated).
func runChecker(ctx context.Context, svc plane.DeriveServices, tool string, args []string) (map[string]*kindVerdict, error) {
	raw, err := svc.Tools.Run(ctx, tool, args, nil)
	if err != nil {
		return nil, err // fail-closed: tool-missing or checker error
	}
	var rep checkerReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("contract: parse %s report: %w", tool, err)
	}
	out := map[string]*kindVerdict{}
	for _, mv := range rep.Modules {
		kv := &kindVerdict{Resolved: mv.Resolved, Reason: mv.Reason, CurrentShape: mv.CurrentShape}
		for _, cr := range mv.Changes {
			kv.Changes = append(kv.Changes, Change{
				Nature:   Nature(cr.Nature),
				Element:  cr.Element,
				Consumer: cr.Consumer,
				Detail:   cr.Detail,
				File:     cr.File,
				Line:     cr.Line,
			})
		}
		out[mv.Module] = kv
	}
	return out, nil
}

// baseline fetches the prior-commit ratified baselines. ANY failure (helper
// unknown in production, file absent in tests, decode error, empty) yields nil —
// no prior, so the intentional-render path simply does not fire. This is never a
// fail-closed condition: the gate decision does not depend on the prior baseline.
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
		km := make(map[string]string, len(bm.Kinds))
		for _, k := range bm.Kinds {
			km[k.Kind] = digest([]byte(k.Baseline))
		}
		m[bm.Module] = km
	}
	return m
}

// readBaselines reads a module's git-tracked ratified baselines under
// <moduleDir>/.grip/contract/<kind>.contract. A missing directory or unreadable
// entry simply contributes no baseline (the kind is then treated as un-ratified —
// cannot-verify if governed). The file's bytes ARE the declared contract; the
// plane never keeps baselines in engine state, which is what makes drift
// impossible by construction.
func readBaselines(moduleDir string) map[string][]byte {
	if moduleDir == "" {
		return nil
	}
	dir := filepath.Join(moduleDir, filepath.FromSlash(baselineDir))
	out := map[string][]byte{}
	for _, kind := range kindsInOrder {
		b, err := os.ReadFile(filepath.Join(dir, kind+baselineExt))
		if err != nil {
			continue
		}
		out[kind] = b
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// apiToolName maps a language to its api contract checker (M0/M1 support PHP + TS).
func apiToolName(lang string) (string, bool) {
	switch lang {
	case "typescript":
		return toolAPITypeScript, true
	case "php":
		return toolAPIPHP, true
	case "go":
		return toolAPIGo, true
	default:
		return "", false
	}
}

// helperArgs mirrors the other planes' convention: the repo root plus one
// language's roots.
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

// allRootsArgs passes the repo root plus every language's roots, for the
// language-neutral events/db checkers. Roots are sorted for a stable argv.
func allRootsArgs(svc plane.DeriveServices) []string {
	args := []string{"--repo-root", svc.RepoRoot}
	specs := append([]plane.LanguageSpec(nil), svc.Languages...)
	sort.Slice(specs, func(a, b int) bool { return specs[a].Language < specs[b].Language })
	for _, s := range specs {
		for _, r := range s.Roots {
			args = append(args, "--root", r)
		}
	}
	return args
}

// digest is the content address of a baseline artifact. Reconcile compares
// digests, never raw text, so the intentional-render check is cheap; equal digest
// ⇔ equal baseline.
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
