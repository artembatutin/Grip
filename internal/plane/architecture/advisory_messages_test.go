package architecture

import "testing"

// TestAdvisoryMessagesGolden pins the exact wording of every Tier B advisory. Tier
// B is deterministic, so — like the Tier A messages — the strings are golden and a
// change is a visible, reviewed diff (NFR-5). (Tier C is non-deterministic and is
// deliberately NOT pinned; see judgment_test.go.)
func TestAdvisoryMessagesGolden(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "duplication",
			got:  msgDuplication(DuplicationSignal{Lines: 12, Modules: []string{"src/a", "src/b"}, Locs: []Loc{{Module: "src/a", File: "src/a/x.ts", Line: 1}, {Module: "src/b", File: "src/b/y.ts", Line: 9}}}),
			want: "modules src/a, src/b share 12 lines of duplicated code (e.g. src/a/x.ts:1 and src/b/y.ts:9) — extract the shared logic into one module both depend on.",
		},
		{
			name: "co-change",
			got:  msgCoChange(CoChangeSignal{A: "src/a", B: "src/b", Together: 8, Total: 10}),
			want: "modules src/a and src/b changed together in 8 of 10 commits but neither declares a dependency on the other — make the coupling explicit in a grip.yaml or decouple them.",
		},
		{
			name: "middle-man",
			got:  msgMiddleMan(MiddleManSignal{Module: "src/a", Forwards: 6, Methods: 8}),
			want: "module src/a forwards 6 of its 8 methods to other modules — it may be a middle man; inline it or give it behavior of its own.",
		},
		{
			name: "message-chain",
			got:  msgMessageChain(ChainSignal{Module: "src/a", File: "src/a/x.ts", Line: 5, Length: 4}),
			want: "a message chain of length 4 at src/a/x.ts:5 reaches across module boundaries — add a method on the first object so callers do not navigate its internals.",
		},
		{
			name: "speculative-generality",
			got:  msgSpeculative(AbstractionSignal{Name: "Repo", Module: "src/a", File: "src/a/x.ts", Line: 2, Implementors: 1}),
			want: "abstraction Repo in module src/a at src/a/x.ts:2 has a single implementor — it may be speculative generality; remove the indirection until a second implementor exists.",
		},
		{
			name: "complexity",
			got:  msgComplexity(ComplexitySignal{Function: "handle", Module: "src/a", File: "src/a/x.ts", Line: 20, Complexity: 15}),
			want: "function handle at src/a/x.ts:20 has cyclomatic complexity 15 (advisory threshold 10) — break it into smaller functions.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("message mismatch:\n got: %s\nwant: %s", tc.got, tc.want)
			}
		})
	}
}
