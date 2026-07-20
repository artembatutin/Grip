// Package report renders one violation/decision model three ways (plan/01 §9):
// a human report (one plain sentence per finding, blocks first), stable JSON for
// tooling, and SARIF for GitHub/GitLab inline annotations. Intentional manifest
// edits are rendered as intentional, never as mystery violations (principle 5).
package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/diff"
	"github.com/artembatutin/grip/internal/gate"
	"github.com/artembatutin/grip/internal/plane"
)

// View bundles everything the renderers need: the gate outcome, an optional shape
// delta, and the declared surfaces (facade/allow per module) the read-only viewer
// overlays on the derived graph. Declared is plain data supplied by the CLI so the
// report package stays plane-agnostic.
type View struct {
	Outcome  *gate.Outcome
	Delta    *diff.Delta
	Declared map[string]Surface
}

// Human renders the default terminal report.
func Human(v View) string {
	var b strings.Builder
	o := v.Outcome

	if o.Decision == "block" {
		fmt.Fprintf(&b, "grip: BLOCKED (exit %d)\n", o.ExitCode)
	} else {
		fmt.Fprintf(&b, "grip: PASS (exit %d)\n", o.ExitCode)
	}
	fmt.Fprintf(&b, "planes: %s · governed modules: %d · ungoverned: %d\n",
		strings.Join(o.PlanesRun, ", "), len(o.Governed), len(o.Ungoverned))

	// Fail-closed reasons lead — they are environment/analysis blocks, not code.
	blocks, advisories, judgment, cannot, intentional := partition(o.Violations)
	if len(o.FailClosed) > 0 || len(cannot) > 0 {
		fmt.Fprintf(&b, "\nfail-closed (%d):\n", len(o.FailClosed)+len(cannot))
		for _, fc := range o.FailClosed {
			fmt.Fprintf(&b, "  ✖ [%s] %s\n", fc.Code, fc.Message)
		}
		for _, vi := range cannot {
			fmt.Fprintf(&b, "  ✖ %s\n", vi.Message)
		}
	}

	if len(blocks) > 0 {
		fmt.Fprintf(&b, "\nblocking violations (%d):\n", len(blocks))
		for _, vi := range blocks {
			fmt.Fprintf(&b, "  ✖ %s\n", line(vi))
		}
	}
	if len(advisories) > 0 {
		fmt.Fprintf(&b, "\nadvisories (%d):\n", len(advisories))
		for _, vi := range advisories {
			fmt.Fprintf(&b, "  • %s\n", line(vi))
		}
	}
	// Tier C judgment is the only place an LLM enters Grip. It is ALWAYS advisory
	// and can never gate a merge, so it is rendered in its own clearly-labeled,
	// non-blocking section (GR-X-6) — never mixed in with anything that can block.
	if len(judgment) > 0 {
		fmt.Fprintf(&b, "\njudgment (%d, non-blocking — advisory only, never gates):\n", len(judgment))
		for _, vi := range judgment {
			fmt.Fprintf(&b, "  ? %s\n", line(vi))
		}
	}

	if v.Delta != nil && !v.Delta.Empty() {
		b.WriteString("\nshape delta:\n")
		writeDelta(&b, v.Delta)
	}
	if len(intentional) > 0 {
		b.WriteString("\nintentional changes:\n")
		for _, vi := range intentional {
			fmt.Fprintf(&b, "  ~ %s\n", vi.Message)
		}
	}

	if len(o.Ungoverned) > 0 {
		fmt.Fprintf(&b, "\nungoverned modules (no grip.yaml): %s\n", strings.Join(o.Ungoverned, ", "))
	}
	return b.String()
}

func line(v plane.Violation) string {
	loc := v.Location.Module
	if v.Location.File != "" {
		loc = fmt.Sprintf("%s (%s:%d)", v.Location.Module, v.Location.File, v.Location.Line)
	}
	return fmt.Sprintf("[%s] %s — %s", v.RuleID, loc, v.Message)
}

func partition(vs []plane.Violation) (blocks, advisories, judgment, cannot, intentional []plane.Violation) {
	for _, v := range vs {
		switch {
		case v.Tier == plane.TierC:
			// Tier C is judgment-assisted (LLM) and never gates; it is reported in
			// its own section, never as a block or an ordinary advisory.
			judgment = append(judgment, v)
		case v.Kind == plane.KindCannotVerify:
			cannot = append(cannot, v)
		case v.Kind == plane.KindIntentionalChange:
			intentional = append(intentional, v)
		case v.Tier == plane.TierA || v.Kind == plane.KindStaleDeclaration:
			blocks = append(blocks, v)
		default:
			advisories = append(advisories, v)
		}
	}
	return
}

func writeDelta(b *strings.Builder, d *diff.Delta) {
	for _, m := range d.ModulesAdded {
		fmt.Fprintf(b, "  + module %s\n", m)
	}
	for _, m := range d.ModulesRemoved {
		fmt.Fprintf(b, "  - module %s\n", m)
	}
	for _, e := range d.EdgesAdded {
		fmt.Fprintf(b, "  + edge %s → %s\n", e.From, e.To)
	}
	for _, e := range d.EdgesRemoved {
		fmt.Fprintf(b, "  - edge %s → %s\n", e.From, e.To)
	}
	for _, s := range d.SurfaceWidened {
		fmt.Fprintf(b, "  + surface %s exposes %s\n", s.Module, strings.Join(s.Added, ", "))
	}
	for _, s := range d.SurfaceNarrowed {
		fmt.Fprintf(b, "  - surface %s hides %s\n", s.Module, strings.Join(s.Removed, ", "))
	}
	for _, c := range d.FacadeEdited {
		fmt.Fprintf(b, "  ~ the architect edited %s's facade on purpose (%s)\n", c.Module, declSummary(c))
	}
	for _, c := range d.AllowEdited {
		fmt.Fprintf(b, "  ~ the architect edited %s's allowed dependencies on purpose (%s)\n", c.Module, declSummary(c))
	}
	for _, c := range d.CyclesAdded {
		fmt.Fprintf(b, "  + cycle %s\n", strings.Join(c, " → "))
	}
	for _, c := range d.CyclesRemoved {
		fmt.Fprintf(b, "  - cycle %s\n", strings.Join(c, " → "))
	}
}

func declSummary(c diff.DeclChange) string {
	var parts []string
	if len(c.Added) > 0 {
		parts = append(parts, "added "+strings.Join(c.Added, ", "))
	}
	if len(c.Removed) > 0 {
		parts = append(parts, "removed "+strings.Join(c.Removed, ", "))
	}
	return strings.Join(parts, "; ")
}

// JSON renders the machine-readable report with a stable schema. It is the exact
// document the read-only viewer consumes; HTML() renders the same Document.
func JSON(v View) ([]byte, error) {
	b, err := json.MarshalIndent(BuildDocument(v), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// SARIF renders SARIF 2.1.0 so blocks show inline on PRs/MRs.
func SARIF(v View) ([]byte, error) {
	rulesSeen := map[string]bool{}
	var rules []sarifRule
	var results []sarifResult
	for _, vi := range v.Outcome.Violations {
		if !rulesSeen[vi.RuleID] {
			rulesSeen[vi.RuleID] = true
			rules = append(rules, sarifRule{ID: vi.RuleID, Name: vi.RuleID,
				ShortDescription: sarifText{Text: vi.RuleID}})
		}
		level := "error"
		if vi.Tier == plane.TierB {
			level = "warning"
		}
		if vi.Tier == plane.TierC {
			level = "note" // judgment-assisted, never blocking
		}
		res := sarifResult{
			RuleID:  vi.RuleID,
			Level:   level,
			Message: sarifText{Text: vi.Message},
		}
		if vi.Location.File != "" {
			res.Locations = []sarifLocation{{
				PhysicalLocation: sarifPhysical{
					ArtifactLocation: sarifArtifact{URI: vi.Location.File},
					Region:           sarifRegion{StartLine: max1(vi.Location.Line)},
				},
			}}
		}
		results = append(results, res)
	}
	// Fail-closed reasons become results too so CI surfaces them.
	for _, fc := range v.Outcome.FailClosed {
		id := "grip." + fc.Code
		if !rulesSeen[id] {
			rulesSeen[id] = true
			rules = append(rules, sarifRule{ID: id, Name: id, ShortDescription: sarifText{Text: fc.Code}})
		}
		results = append(results, sarifResult{RuleID: id, Level: "error", Message: sarifText{Text: fc.Message}})
	}
	sort.Slice(rules, func(a, b int) bool { return rules[a].ID < rules[b].ID })

	doc := sarifLog{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "grip",
				InformationURI: "https://github.com/artembatutin/grip",
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
