package gate

import (
	"sort"

	"github.com/artembatutin/grip/internal/plane"
)

// SortViolations orders violations canonically so reports are byte-stable across
// runs (NFR-1): by tier (A before B before C), then rule id, module, file, line,
// and finally the message to fully break ties.
func SortViolations(vs []plane.Violation) {
	sort.SliceStable(vs, func(a, b int) bool {
		x, y := vs[a], vs[b]
		if x.Tier != y.Tier {
			return x.Tier < y.Tier
		}
		if x.RuleID != y.RuleID {
			return x.RuleID < y.RuleID
		}
		if x.Location.Module != y.Location.Module {
			return x.Location.Module < y.Location.Module
		}
		if x.Location.File != y.Location.File {
			return x.Location.File < y.Location.File
		}
		if x.Location.Line != y.Location.Line {
			return x.Location.Line < y.Location.Line
		}
		return x.Message < y.Message
	})
}
