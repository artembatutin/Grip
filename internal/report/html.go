// This file renders the read-only visualization (M4 Part B, GR-X-7, NFR-10). It is
// a FOURTH rendering of the same decision model (after Human, JSON, SARIF): a
// self-contained static HTML page that draws the derived graph with manifest
// overlays (allowed vs actual, violations highlighted) and the shape diff.
//
// It is strictly read-only and strictly derived (PRD §4.3, ACP §11.1):
//   - It is a pure function of the Document — the same bytes JSON() emits. It has
//     no source of truth of its own and no server: HTML(doc) in, a string out.
//   - It contains NO executable script, NO form/input/textarea/button, NO
//     contenteditable, and NO network access. There is therefore no affordance to
//     change a manifest, an edge, or a facade through the visual. That is the hard
//     guardrail for this phase, and TestHTMLHasNoEditingAffordance enforces it.
//   - It is plane-agnostic (engine-core purity): it colors by Tier, never by a
//     plane's rule id, and names no plane.
//
// There is no editable authoritative diagram here, and there is no M5 studio.

package report

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/plane"
)

// HTML renders the read-only viewer for a report Document. Deterministic: the same
// Document yields byte-identical output (every iteration is over sorted data).
func HTML(doc Document) string {
	var b strings.Builder
	writeHTMLHead(&b, doc)
	writeBanner(&b, doc)
	writeGraph(&b, doc)
	writeViolations(&b, doc)
	writeDiff(&b, doc)
	writeSource(&b, doc)
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

// --- layout constants ---
const (
	svgMargin = 24
	nodeW     = 200
	nodeH     = 68
	gapX      = 72
	gapY      = 56
	cols      = 3
)

type nodePos struct {
	x, y       int
	cx, cy     int
	governed   bool
	tierClass  string // css class for the worst violation tier on this module
	layer      string
	facadeMore int // count of derived-reachable symbols not in the declared facade
}

func writeHTMLHead(b *strings.Builder, doc Document) {
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>grip — architecture view</title>\n")
	b.WriteString("<style>\n")
	b.WriteString(css)
	b.WriteString("\n</style>\n</head>\n<body>\n<main>\n")
}

func writeBanner(b *strings.Builder, doc Document) {
	cls := "pass"
	word := "PASS"
	if doc.Decision == "block" {
		cls, word = "block", "BLOCKED"
	}
	fmt.Fprintf(b, "<header class=\"banner %s\">\n", cls)
	fmt.Fprintf(b, "<span class=\"decision\">%s</span> <span class=\"exit\">exit %d</span>\n", word, doc.ExitCode)
	fmt.Fprintf(b, "<div class=\"meta\">planes: %s &middot; governed: %d &middot; ungoverned: %d",
		esc(strings.Join(doc.PlanesRun, ", ")), len(doc.Governed), len(doc.Ungoverned))
	if doc.IRHash != "" {
		fmt.Fprintf(b, " &middot; ir %s", esc(short(doc.IRHash)))
	}
	b.WriteString("</div>\n</header>\n")
	b.WriteString("<p class=\"note\">Read-only. This view is derived from the gate's JSON report; it cannot change any manifest, edge, or facade.</p>\n")
}

// writeGraph draws the derived module graph with the allowed-vs-actual edge overlay
// and per-module violation highlighting.
func writeGraph(b *strings.Builder, doc Document) {
	b.WriteString("<section>\n<h2>Derived graph</h2>\n")
	if doc.Graph == nil || len(doc.Graph.Modules)+len(doc.Ungoverned) == 0 {
		b.WriteString("<p class=\"empty\">No derived graph in this report.</p>\n</section>\n")
		return
	}

	worst := worstTierByModule(doc.Violations)
	pos := layoutNodes(doc, worst)

	// order node ids deterministically
	ids := make([]string, 0, len(pos))
	for id := range pos {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	width, height := svgExtent(pos)
	fmt.Fprintf(b, "<svg class=\"graph\" viewBox=\"0 0 %d %d\" role=\"img\" aria-label=\"derived module graph\">\n", width, height)
	b.WriteString("<defs>\n")
	b.WriteString("<marker id=\"arrow-ok\" viewBox=\"0 0 10 10\" refX=\"9\" refY=\"5\" markerWidth=\"7\" markerHeight=\"7\" orient=\"auto-start-reverse\"><path d=\"M0,0 L10,5 L0,10 z\" fill=\"#64748b\"/></marker>\n")
	b.WriteString("<marker id=\"arrow-drift\" viewBox=\"0 0 10 10\" refX=\"9\" refY=\"5\" markerWidth=\"7\" markerHeight=\"7\" orient=\"auto-start-reverse\"><path d=\"M0,0 L10,5 L0,10 z\" fill=\"#dc2626\"/></marker>\n")
	b.WriteString("</defs>\n")

	// edges first (under nodes). Graph.Edges is canonically sorted.
	for _, e := range doc.Graph.Edges {
		from, okF := pos[e.From]
		to, okT := pos[e.To]
		if !okF || !okT {
			continue
		}
		allowed := edgeAllowed(doc, e.From, e.To)
		cls, marker := "edge-drift", "arrow-drift"
		if allowed {
			cls, marker = "edge-ok", "arrow-ok"
		}
		fmt.Fprintf(b, "<line class=\"%s\" x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" marker-end=\"url(#%s)\"><title>%s</title></line>\n",
			cls, from.cx, from.cy, to.cx, to.cy, marker, esc(edgeTitle(e.From, e.To, allowed)))
	}

	// nodes on top
	for _, id := range ids {
		p := pos[id]
		cls := "node " + p.tierClass
		if !p.governed {
			cls = "node ungoverned"
		}
		fmt.Fprintf(b, "<g class=\"%s\">\n", cls)
		fmt.Fprintf(b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"8\"/>\n", p.x, p.y, nodeW, nodeH)
		fmt.Fprintf(b, "<text class=\"node-id\" x=\"%d\" y=\"%d\">%s</text>\n", p.x+12, p.y+24, esc(id))
		sub := nodeSubtitle(p)
		fmt.Fprintf(b, "<text class=\"node-sub\" x=\"%d\" y=\"%d\">%s</text>\n", p.x+12, p.y+44, esc(sub))
		if p.facadeMore > 0 {
			fmt.Fprintf(b, "<text class=\"node-warn\" x=\"%d\" y=\"%d\">+%d beyond facade</text>\n", p.x+12, p.y+60, p.facadeMore)
		}
		b.WriteString("</g>\n")
	}
	b.WriteString("</svg>\n")
	writeLegend(b)
	b.WriteString("</section>\n")
}

func writeLegend(b *strings.Builder) {
	b.WriteString("<ul class=\"legend\">\n")
	b.WriteString("<li><span class=\"swatch edge-ok\"></span>allowed dependency</li>\n")
	b.WriteString("<li><span class=\"swatch edge-drift\"></span>actual dependency not in the manifest (drift)</li>\n")
	b.WriteString("<li><span class=\"swatch tier-a\"></span>blocking violation</li>\n")
	b.WriteString("<li><span class=\"swatch tier-b\"></span>advisory (non-blocking unless promoted)</li>\n")
	b.WriteString("<li><span class=\"swatch tier-c\"></span>judgment (never gates)</li>\n")
	b.WriteString("<li><span class=\"swatch ungoverned\"></span>ungoverned (no grip.yaml)</li>\n")
	b.WriteString("</ul>\n")
}

func writeViolations(b *strings.Builder, doc Document) {
	blocks, advisories, judgment, cannot, intentional := partition(doc.Violations)
	b.WriteString("<section>\n<h2>Findings</h2>\n")
	if len(doc.FailClosed) > 0 || len(cannot) > 0 {
		fmt.Fprintf(b, "<h3 class=\"tier-a\">fail-closed (%d)</h3>\n<ul class=\"findings\">\n", len(doc.FailClosed)+len(cannot))
		for _, fc := range doc.FailClosed {
			fmt.Fprintf(b, "<li class=\"tier-a\">[%s] %s</li>\n", esc(fc.Code), esc(fc.Message))
		}
		writeFindingItems(b, cannot, "tier-a")
		b.WriteString("</ul>\n")
	}
	writeFindingGroup(b, "blocking violations", "tier-a", blocks)
	writeFindingGroup(b, "advisories (non-blocking unless promoted)", "tier-b", advisories)
	writeFindingGroup(b, "judgment (non-blocking — advisory only, never gates)", "tier-c", judgment)
	writeFindingGroup(b, "intentional changes", "intentional", intentional)
	if len(blocks)+len(advisories)+len(judgment)+len(cannot)+len(intentional)+len(doc.FailClosed) == 0 {
		b.WriteString("<p class=\"empty\">No findings.</p>\n")
	}
	b.WriteString("</section>\n")
}

func writeFindingGroup(b *strings.Builder, title, cls string, vs []plane.Violation) {
	if len(vs) == 0 {
		return
	}
	fmt.Fprintf(b, "<h3 class=\"%s\">%s (%d)</h3>\n<ul class=\"findings\">\n", cls, esc(title), len(vs))
	writeFindingItems(b, vs, cls)
	b.WriteString("</ul>\n")
}

func writeFindingItems(b *strings.Builder, vs []plane.Violation, cls string) {
	for _, vi := range vs {
		fmt.Fprintf(b, "<li class=\"%s\"><span class=\"rule\">%s</span> <span class=\"loc\">%s</span> %s</li>\n",
			cls, esc(vi.RuleID), esc(locText(vi)), esc(vi.Message))
	}
}

func writeDiff(b *strings.Builder, doc Document) {
	b.WriteString("<section>\n<h2>Shape diff</h2>\n")
	d := doc.Delta
	if d == nil || d.Empty() {
		b.WriteString("<p class=\"empty\">No shape delta (no baseline, or no change vs baseline).</p>\n</section>\n")
		return
	}
	b.WriteString("<ul class=\"diff\">\n")
	for _, m := range d.ModulesAdded {
		fmt.Fprintf(b, "<li class=\"add\">+ module %s</li>\n", esc(m))
	}
	for _, m := range d.ModulesRemoved {
		fmt.Fprintf(b, "<li class=\"rem\">- module %s</li>\n", esc(m))
	}
	for _, e := range d.EdgesAdded {
		fmt.Fprintf(b, "<li class=\"add\">+ edge %s &rarr; %s</li>\n", esc(e.From), esc(e.To))
	}
	for _, e := range d.EdgesRemoved {
		fmt.Fprintf(b, "<li class=\"rem\">- edge %s &rarr; %s</li>\n", esc(e.From), esc(e.To))
	}
	for _, s := range d.SurfaceWidened {
		fmt.Fprintf(b, "<li class=\"add\">+ surface %s exposes %s</li>\n", esc(s.Module), esc(strings.Join(s.Added, ", ")))
	}
	for _, s := range d.SurfaceNarrowed {
		fmt.Fprintf(b, "<li class=\"rem\">- surface %s hides %s</li>\n", esc(s.Module), esc(strings.Join(s.Removed, ", ")))
	}
	for _, c := range d.FacadeEdited {
		fmt.Fprintf(b, "<li class=\"intentional\">~ %s facade edited on purpose (%s)</li>\n", esc(c.Module), esc(declText(c.Added, c.Removed)))
	}
	for _, c := range d.AllowEdited {
		fmt.Fprintf(b, "<li class=\"intentional\">~ %s allowed dependencies edited on purpose (%s)</li>\n", esc(c.Module), esc(declText(c.Added, c.Removed)))
	}
	for _, c := range d.CyclesAdded {
		fmt.Fprintf(b, "<li class=\"add\">+ cycle %s</li>\n", esc(strings.Join(c, " → ")))
	}
	for _, c := range d.CyclesRemoved {
		fmt.Fprintf(b, "<li class=\"rem\">- cycle %s</li>\n", esc(strings.Join(c, " → ")))
	}
	b.WriteString("</ul>\n</section>\n")
}

// writeSource embeds the report's own JSON, escaped into a disclosure block, so the
// page visibly is what it claims: a pure rendering of the JSON. A <details>
// disclosure is read-only — it reveals content, it never edits it.
func writeSource(b *strings.Builder, doc Document) {
	js, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	b.WriteString("<details>\n<summary>report JSON (this page is a pure rendering of it)</summary>\n<pre>")
	b.WriteString(esc(string(js)))
	b.WriteString("</pre>\n</details>\n")
}

// --- helpers ---

func layoutNodes(doc Document, worst map[string]plane.Tier) map[string]nodePos {
	facadeMore := reachBeyondFacade(doc)
	governed := map[string]bool{}
	var ids []string
	for _, m := range doc.Graph.Modules {
		governed[m.ID] = true
		ids = append(ids, m.ID)
	}
	for _, id := range doc.Ungoverned {
		if !governed[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	pos := make(map[string]nodePos, len(ids))
	for i, id := range ids {
		col := i % cols
		row := i / cols
		x := svgMargin + col*(nodeW+gapX)
		y := svgMargin + row*(nodeH+gapY)
		np := nodePos{
			x: x, y: y, cx: x + nodeW/2, cy: y + nodeH/2,
			governed:   governed[id],
			tierClass:  tierClass(worst, id),
			facadeMore: facadeMore[id],
		}
		if m := doc.Graph.Module(id); m != nil {
			np.layer = m.Layer
		}
		pos[id] = np
	}
	return pos
}

func svgExtent(pos map[string]nodePos) (w, h int) {
	w, h = nodeW+2*svgMargin, nodeH+2*svgMargin
	for _, p := range pos {
		if p.x+nodeW+svgMargin > w {
			w = p.x + nodeW + svgMargin
		}
		if p.y+nodeH+svgMargin > h {
			h = p.y + nodeH + svgMargin
		}
	}
	return w, h
}

// worstTierByModule maps a module id to the most severe violation tier on it
// (Tier A most severe). cannot-verify counts as Tier A severity (it blocks).
func worstTierByModule(vs []plane.Violation) map[string]plane.Tier {
	out := map[string]plane.Tier{}
	for _, v := range vs {
		m := v.Location.Module
		if m == "" {
			continue
		}
		t := v.Tier
		if v.Kind == plane.KindCannotVerify {
			t = plane.TierA
		}
		if cur, ok := out[m]; !ok || t < cur {
			out[m] = t // lower Tier value == more severe
		}
	}
	return out
}

func tierClass(worst map[string]plane.Tier, id string) string {
	t, ok := worst[id]
	if !ok {
		return "clean"
	}
	switch t {
	case plane.TierA:
		return "tier-a"
	case plane.TierB:
		return "tier-b"
	case plane.TierC:
		return "tier-c"
	default:
		return "clean"
	}
}

// edgeAllowed reports whether a derived edge from->to is covered by the declared
// allow set of `from` (either naming the target module or its layer). This is the
// "allowed vs actual" overlay, computed purely from the Document.
func edgeAllowed(doc Document, from, to string) bool {
	decl, ok := doc.Declared[from]
	if !ok {
		return false
	}
	for _, a := range decl.Allow {
		if a == to {
			return true
		}
	}
	if doc.Graph != nil {
		if m := doc.Graph.Module(to); m != nil && m.Layer != "" {
			for _, a := range decl.Allow {
				if a == m.Layer {
					return true
				}
			}
		}
	}
	return false
}

// reachBeyondFacade counts, per module, the derived reachable-from-outside symbols
// that are not in the declared facade (the facade-widening overlay).
func reachBeyondFacade(doc Document) map[string]int {
	out := map[string]int{}
	if doc.Graph == nil {
		return out
	}
	for _, m := range doc.Graph.Modules {
		facade := map[string]bool{}
		for _, f := range doc.Declared[m.ID].Facade {
			facade[f] = true
		}
		n := 0
		for _, s := range m.ReachableFromOutside {
			if !facade[s] {
				n++
			}
		}
		out[m.ID] = n
	}
	return out
}

func nodeSubtitle(p nodePos) string {
	if !p.governed {
		return "ungoverned"
	}
	if p.layer != "" {
		return "layer: " + p.layer
	}
	return "governed"
}

func edgeTitle(from, to string, allowed bool) string {
	if allowed {
		return from + " → " + to + " (allowed)"
	}
	return from + " → " + to + " (not declared — drift)"
}

func locText(v plane.Violation) string {
	if v.Location.File != "" {
		return fmt.Sprintf("%s (%s:%d)", v.Location.Module, v.Location.File, v.Location.Line)
	}
	return v.Location.Module
}

func declText(added, removed []string) string {
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "added "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "removed "+strings.Join(removed, ", "))
	}
	return strings.Join(parts, "; ")
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func esc(s string) string { return html.EscapeString(s) }

const css = `:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;font:14px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;color:#0f172a;background:#f8fafc}
main{max-width:960px;margin:0 auto;padding:24px}
h2{font-size:16px;margin:28px 0 8px;border-bottom:1px solid #e2e8f0;padding-bottom:4px}
h3{font-size:13px;margin:16px 0 6px;text-transform:uppercase;letter-spacing:.04em}
.banner{display:flex;align-items:baseline;gap:12px;flex-wrap:wrap;padding:14px 16px;border-radius:10px;color:#fff}
.banner.pass{background:#16a34a}
.banner.block{background:#dc2626}
.banner .decision{font-weight:700;font-size:18px}
.banner .exit{opacity:.9}
.banner .meta{flex-basis:100%;font-size:12px;opacity:.95}
.note{color:#64748b;font-size:12px;margin:8px 2px}
.empty{color:#64748b}
svg.graph{width:100%;height:auto;background:#fff;border:1px solid #e2e8f0;border-radius:10px}
.node rect{fill:#fff;stroke:#16a34a;stroke-width:2}
.node.tier-a rect{stroke:#dc2626}
.node.tier-b rect{stroke:#d97706}
.node.tier-c rect{stroke:#2563eb}
.node.clean rect{stroke:#16a34a}
.node.ungoverned rect{stroke:#94a3b8;stroke-dasharray:5 4;fill:#f8fafc}
.node-id{font-weight:600;font-size:13px;fill:#0f172a}
.node-sub{font-size:11px;fill:#64748b}
.node-warn{font-size:11px;fill:#dc2626}
line.edge-ok{stroke:#64748b;stroke-width:1.5}
line.edge-drift{stroke:#dc2626;stroke-width:2;stroke-dasharray:6 4}
ul.legend{list-style:none;padding:0;display:flex;flex-wrap:wrap;gap:14px;font-size:12px;color:#475569;margin:10px 2px}
ul.legend li{display:flex;align-items:center;gap:6px}
.swatch{display:inline-block;width:14px;height:14px;border-radius:3px}
.swatch.edge-ok{background:#64748b}
.swatch.edge-drift{background:#dc2626}
.swatch.tier-a{background:#dc2626}
.swatch.tier-b{background:#d97706}
.swatch.tier-c{background:#2563eb}
.swatch.ungoverned{background:#94a3b8}
ul.findings,ul.diff{list-style:none;padding:0;margin:6px 0}
ul.findings li,ul.diff li{padding:6px 10px;border-left:3px solid #cbd5e1;margin:4px 0;background:#fff;border-radius:0 6px 6px 0}
li.tier-a{border-left-color:#dc2626}
li.tier-b{border-left-color:#d97706}
li.tier-c{border-left-color:#2563eb}
li.intentional{border-left-color:#8b5cf6}
li.add{border-left-color:#16a34a}
li.rem{border-left-color:#dc2626}
.rule{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;color:#334155}
.loc{color:#64748b;font-size:12px}
details{margin-top:24px}
summary{cursor:pointer;color:#475569;font-size:12px}
pre{overflow:auto;background:#0f172a;color:#e2e8f0;padding:12px;border-radius:8px;font-size:12px}`
