// Package ratify implements generate-then-ratify onboarding (plan/03 M0.10,
// GR-X-4): accept current reality as the declared start so a brownfield repo goes
// from zero manifests to a green gate in one sitting. It derives the current IR
// and drafts a small grip.yaml per module (facade = current external surface;
// dependencies.allow = current edges; intent = a human-owned placeholder) plus a
// starter .grip.yaml. Drafts are deliberately small (NFR-3) so the human can
// read and adjust intent before governance bites.
package ratify

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/artembatutin/grip/internal/ir"
)

// File is a generated artifact: a repo-relative path and its content.
type File struct {
	Path    string
	Content string
}

// DraftManifests returns one draft grip.yaml per governed-or-derived module in
// the IR. The facade is the module's current external surface (so the baseline
// passes); allow is its current outbound edges; intent is a placeholder.
func DraftManifests(g *ir.Graph) []File {
	var files []File
	// Precompute outbound edges per module.
	outbound := map[string][]string{}
	for _, e := range g.Edges {
		outbound[e.From] = appendUnique(outbound[e.From], e.To)
	}
	mods := append([]ir.Module(nil), g.Modules...)
	sort.Slice(mods, func(a, b int) bool { return mods[a].ID < mods[b].ID })
	for _, m := range mods {
		content := draftManifest(m, outbound[m.ID])
		files = append(files, File{Path: path.Join(m.ID, "grip.yaml"), Content: content})
	}
	return files
}

func draftManifest(m ir.Module, allow []string) string {
	name := path.Base(m.ID)

	// Facade = actual external surface; fall back to all exports for an unused
	// but public module so the human sees the candidates.
	facade := append([]string(nil), m.ReachableFromOutside...)
	if len(facade) == 0 {
		for _, ex := range m.Exports {
			facade = appendUnique(facade, ex.Name)
		}
	}
	sort.Strings(facade)
	sort.Strings(allow)

	var b strings.Builder
	fmt.Fprintf(&b, "module: %s\n", name)
	b.WriteString("intent: >\n")
	fmt.Fprintf(&b, "  TODO: describe %s's single responsibility and what it must NOT do.\n", name)
	b.WriteString("architecture:\n")
	writeList(&b, "facade", facade, 1)
	b.WriteString("  dependencies:\n")
	writeList(&b, "allow", allow, 2)
	b.WriteString("  cycles: forbid\n")
	return b.String()
}

// writeList writes a YAML list under key at the given indent (in 2-space units),
// rendering an empty list as `key: []`.
func writeList(b *strings.Builder, key string, items []string, indent int) {
	pad := strings.Repeat("  ", indent)
	if len(items) == 0 {
		fmt.Fprintf(b, "%s%s: []\n", pad, key)
		return
	}
	fmt.Fprintf(b, "%s%s:\n", pad, key)
	for _, it := range items {
		fmt.Fprintf(b, "%s  - %s\n", pad, it)
	}
}

// StarterConfig returns a draft .grip.yaml enabling the architecture plane over
// the given language roots.
func StarterConfig(languageRoots map[string][]string) string {
	var b strings.Builder
	b.WriteString("version: 1\n")
	b.WriteString("planes:\n")
	b.WriteString("  architecture: { enabled: true }\n\n")
	b.WriteString("languages:\n")
	langs := make([]string, 0, len(languageRoots))
	for l := range languageRoots {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		roots := append([]string(nil), languageRoots[l]...)
		sort.Strings(roots)
		fmt.Fprintf(&b, "  %s:\n", l)
		fmt.Fprintf(&b, "    roots: [%s]\n", strings.Join(quoteAll(roots), ", "))
		fmt.Fprintf(&b, "    tool: { name: %s }\n", defaultTool(l))
	}
	b.WriteString("\nmodules:\n  granularity: directory\n\n")
	b.WriteString("gate:\n  failClosed: true\n  local: { planes: [architecture] }\n  ci: { planes: [architecture] }\n")
	return b.String()
}

func defaultTool(language string) string {
	switch language {
	case "typescript":
		return "dependency-cruiser"
	case "php":
		return "deptrac"
	default:
		return language
	}
}

func quoteAll(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = "\"" + v + "\""
	}
	return out
}

func appendUnique(s []string, v string) []string {
	for _, e := range s {
		if e == v {
			return s
		}
	}
	return append(s, v)
}
