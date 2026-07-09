package architecture

import (
	"sort"
	"strings"
	"testing"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// buildModel assembles a Model from modules and edges for reconcile tests.
func buildModel(mods []ir.Module, edges []ir.Edge, conf []ir.Confidence, layers, ungoverned []string) *Model {
	g := &ir.Graph{IRVersion: ir.Version, Modules: mods, Edges: edges, Confidence: conf}
	g.Canonicalize()
	return &Model{Graph: g, LayerOrder: layers, Ungoverned: ungoverned}
}

func in(id string, facade, allow []string, layer string) Intent {
	return Intent{ModuleID: id, Facade: facade, Allow: allow, Layer: layer, Cycles: cyclesForbid, HasSection: true}
}

func intents(is ...Intent) map[string]Intent {
	m := map[string]Intent{}
	for _, i := range is {
		m[i.ModuleID] = i
	}
	return m
}

func ruleIDs(vs []plane.Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.RuleID+":"+string(v.Kind))
	}
	sort.Strings(out)
	return out
}

// mod is a convenience for a module with entrypoint exports and reachable set.
func mod(id, lang string, exports []ir.Export, reachable []string) ir.Module {
	return ir.Module{ID: id, Language: lang, Exports: exports, ReachableFromOutside: reachable}
}

func edge(from, to, sym, file string, line int) ir.Edge {
	return ir.Edge{From: from, To: to, Kind: "import", Evidence: []ir.Evidence{{File: file, Line: line, Symbol: sym}}}
}

func TestReconcileRules(t *testing.T) {
	orderExport := []ir.Export{{Name: "Order", Kind: "class", File: "src/domain/index.ts", Line: 2}}

	cases := []struct {
		name       string
		model      *Model
		intents    map[string]Intent
		wantRules  []string // rule:kind set expected (sorted); nil = none
		wantMsgSub string   // substring expected in some message
	}{
		{
			name: "clean passes",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, []string{"Order"})},
				[]ir.Edge{edge("src/app", "src/domain", "Order", "src/app/index.ts", 1)},
				nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, "domain"),
				in("src/app", nil, []string{"src/domain"}, "domain"),
			),
			wantRules: nil,
		},
		{
			name: "illegal-dependency fires",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, []string{"Order"})},
				[]ir.Edge{edge("src/app", "src/domain", "Order", "src/app/index.ts", 1)},
				nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, ""),
				in("src/app", nil, nil, ""), // app does NOT allow domain
			),
			wantRules:  []string{"arch.illegal-dependency:violation"},
			wantMsgSub: "not in its allowed dependencies",
		},
		{
			name: "illegal-dependency negative (allowed)",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, []string{"Order"})},
				[]ir.Edge{edge("src/app", "src/domain", "Order", "src/app/index.ts", 1)},
				nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, ""),
				in("src/app", nil, []string{"src/domain"}, ""),
			),
			wantRules: nil,
		},
		{
			name: "facade-widening fires",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript",
					[]ir.Export{{Name: "Order", File: "src/domain/index.ts", Line: 2}, {Name: "OrderId", File: "src/domain/index.ts", Line: 6}},
					[]string{"Order", "OrderId"})},
				[]ir.Edge{edge("src/app", "src/domain", "OrderId", "src/app/index.ts", 1)},
				nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, ""), // facade omits OrderId
				in("src/app", nil, []string{"src/domain"}, ""),
			),
			wantRules:  []string{"arch.facade-widening:violation"},
			wantMsgSub: "not in its declared facade",
		},
		{
			name: "facade-widening negative (declared)",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript",
					[]ir.Export{{Name: "Order", File: "src/domain/index.ts", Line: 2}, {Name: "OrderId", File: "src/domain/index.ts", Line: 6}},
					[]string{"Order", "OrderId"})},
				[]ir.Edge{edge("src/app", "src/domain", "OrderId", "src/app/index.ts", 1)},
				nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order", "OrderId"}, nil, ""),
				in("src/app", nil, []string{"src/domain"}, ""),
			),
			wantRules: nil,
		},
		{
			name: "internal-reach fires (symbol not exported)",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, []string{"Order", "Guts"})},
				[]ir.Edge{edge("src/app", "src/domain", "Guts", "src/app/index.ts", 4)},
				nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, ""),
				in("src/app", nil, []string{"src/domain"}, ""),
			),
			wantRules:  []string{"arch.internal-reach:violation"},
			wantMsgSub: "reaches internal symbol Guts",
		},
		{
			name: "cycle fires",
			model: buildModel(
				[]ir.Module{
					mod("src/a", "typescript", []ir.Export{{Name: "A"}}, []string{"A"}),
					mod("src/b", "typescript", []ir.Export{{Name: "B"}}, []string{"B"}),
				},
				[]ir.Edge{edge("src/a", "src/b", "B", "src/a/i.ts", 1), edge("src/b", "src/a", "A", "src/b/i.ts", 1)},
				nil, nil, nil),
			intents: intents(
				in("src/a", []string{"A"}, []string{"src/b"}, ""),
				in("src/b", []string{"B"}, []string{"src/a"}, ""),
			),
			wantRules:  []string{"arch.cycle:violation"},
			wantMsgSub: "form a dependency cycle",
		},
		{
			name: "direction-violation fires",
			model: buildModel(
				[]ir.Module{{ID: "src/domain"}, {ID: "src/infra", Exports: []ir.Export{{Name: "Db"}}}},
				[]ir.Edge{edge("src/domain", "src/infra", "Db", "src/domain/index.ts", 3)},
				nil, []string{"domain", "infrastructure"}, nil),
			intents: intents(
				in("src/domain", nil, []string{"src/infra"}, "domain"),
				in("src/infra", []string{"Db"}, nil, "infrastructure"),
			),
			wantRules:  []string{"arch.direction-violation:violation"},
			wantMsgSub: "against the declared layer order",
		},
		{
			name: "direction negative (inward is fine)",
			model: buildModel(
				[]ir.Module{{ID: "src/domain", Exports: []ir.Export{{Name: "Order"}}}, {ID: "src/infra"}},
				[]ir.Edge{edge("src/infra", "src/domain", "Order", "src/infra/index.ts", 3)},
				nil, []string{"domain", "infrastructure"}, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, "domain"),
				in("src/infra", nil, []string{"src/domain"}, "infrastructure"),
			),
			wantRules: nil,
		},
		{
			name: "stale-declaration fires (facade lists deleted export)",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, nil)},
				nil, nil, nil, nil),
			intents: intents(
				in("src/domain", []string{"Order", "Ghost"}, nil, ""),
			),
			wantRules:  []string{"arch.stale-declaration:staleDeclaration"},
			wantMsgSub: "no longer exists as an export",
		},
		{
			name: "missing-manifest fires (allow references ungoverned)",
			model: buildModel(
				[]ir.Module{mod("src/app", "typescript", nil, nil)},
				nil, nil, nil, []string{"src/legacy"}),
			intents: intents(
				in("src/app", nil, []string{"src/legacy"}, ""),
			),
			wantRules:  []string{"arch.illegal-dependency:cannotVerify"},
			wantMsgSub: "ungoverned module with no grip.yaml",
		},
		{
			name: "cannot-verify fires (reduced scope in governed module)",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, nil)},
				nil,
				[]ir.Confidence{{Scope: "src/domain/dyn.ts", Level: ir.LevelReduced, Reason: "dynamic dispatch"}},
				nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, ""),
			),
			wantRules:  []string{"arch.illegal-dependency:cannotVerify"},
			wantMsgSub: "cannot verify",
		},
		{
			name: "cannot-verify negative (reduced scope in ungoverned area)",
			model: buildModel(
				[]ir.Module{mod("src/domain", "typescript", orderExport, nil)},
				nil,
				[]ir.Confidence{{Scope: "src/legacy/loader.ts", Level: ir.LevelReduced, Reason: "dynamic import()"}},
				nil, nil),
			intents: intents(
				in("src/domain", []string{"Order"}, nil, ""),
			),
			wantRules: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reconcile(tc.intents, tc.model)
			if diff := compareRuleSets(ruleIDs(got), tc.wantRules); diff != "" {
				t.Errorf("rules mismatch: %s\nviolations: %+v", diff, got)
			}
			if tc.wantMsgSub != "" && !anyMsgContains(got, tc.wantMsgSub) {
				t.Errorf("no message contains %q; got %+v", tc.wantMsgSub, got)
			}
		})
	}
}

// TestReconcileDeterministic asserts reconcile is order-independent: shuffling
// modules/edges yields byte-identical violation output.
func TestReconcileDeterministic(t *testing.T) {
	m := buildModel(
		[]ir.Module{
			mod("src/a", "typescript", []ir.Export{{Name: "A"}}, []string{"A", "Secret"}),
			mod("src/b", "typescript", []ir.Export{{Name: "B"}}, nil),
		},
		[]ir.Edge{
			edge("src/b", "src/a", "Secret", "src/b/i.ts", 1),
			edge("src/a", "src/b", "B", "src/a/i.ts", 1),
		},
		nil, nil, nil)
	is := intents(
		in("src/a", []string{"A"}, []string{"src/b"}, ""),
		in("src/b", []string{"B"}, nil, ""), // b does not allow a -> illegal
	)
	first := ruleIDs(reconcile(is, m))
	for i := 0; i < 20; i++ {
		// Reverse module + edge order each time.
		reverseModules(m.Graph)
		reverseEdges(m.Graph)
		got := ruleIDs(reconcile(is, m))
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("iteration %d: %v != %v", i, got, first)
		}
	}
}

func reverseModules(g *ir.Graph) {
	for i, j := 0, len(g.Modules)-1; i < j; i, j = i+1, j-1 {
		g.Modules[i], g.Modules[j] = g.Modules[j], g.Modules[i]
	}
}
func reverseEdges(g *ir.Graph) {
	for i, j := 0, len(g.Edges)-1; i < j; i, j = i+1, j-1 {
		g.Edges[i], g.Edges[j] = g.Edges[j], g.Edges[i]
	}
}

func compareRuleSets(got, want []string) string {
	if len(got) != len(want) {
		return "got " + strings.Join(got, ",") + " want " + strings.Join(want, ",")
	}
	for i := range got {
		if got[i] != want[i] {
			return "got " + strings.Join(got, ",") + " want " + strings.Join(want, ",")
		}
	}
	return ""
}

func anyMsgContains(vs []plane.Violation, sub string) bool {
	for _, v := range vs {
		if strings.Contains(v.Message, sub) {
			return true
		}
	}
	return false
}
