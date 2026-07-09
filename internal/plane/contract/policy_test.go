package contract

import "testing"

// TestClassifyTable is the exhaustive, mutation-oriented test of the policy core:
// every (nature, compat) cell has an asserted verdict. Flipping ANY cell in
// classify() — or the fail-closed default for an unknown nature — makes exactly one
// row here fail, which is the point: the policy is the plane's whole judgment, so
// it must be pinned cell by cell. The required-added/narrowed rows under forward vs
// backward are the sharp policy near-miss: the SAME change flips verdict with the
// policy, proving the policy (not the checker) decides.
func TestClassifyTable(t *testing.T) {
	cases := []struct {
		nature Nature
		compat Compat
		want   Verdict
	}{
		// Removed / renamed / destructive break under every policy.
		{NatureRemoved, CompatBackward, VerdictBreaking},
		{NatureRemoved, CompatForward, VerdictBreaking},
		{NatureRemoved, CompatFull, VerdictBreaking},
		{NatureRenamed, CompatBackward, VerdictBreaking},
		{NatureRenamed, CompatForward, VerdictBreaking},
		{NatureRenamed, CompatFull, VerdictBreaking},
		{NatureDestructive, CompatBackward, VerdictBreaking},
		{NatureDestructive, CompatForward, VerdictBreaking},
		{NatureDestructive, CompatFull, VerdictBreaking},

		// Required-added / narrowed: break under backward & full, safe under forward.
		{NatureRequiredAdded, CompatBackward, VerdictBreaking},
		{NatureRequiredAdded, CompatForward, VerdictCompatible},
		{NatureRequiredAdded, CompatFull, VerdictBreaking},
		{NatureNarrowed, CompatBackward, VerdictBreaking},
		{NatureNarrowed, CompatForward, VerdictCompatible},
		{NatureNarrowed, CompatFull, VerdictBreaking},

		// Additive / deprecation: advisory under every policy.
		{NatureOptionalAdded, CompatBackward, VerdictAdditive},
		{NatureOptionalAdded, CompatForward, VerdictAdditive},
		{NatureOptionalAdded, CompatFull, VerdictAdditive},
		{NatureDeprecation, CompatBackward, VerdictAdditive},
		{NatureDeprecation, CompatForward, VerdictAdditive},
		{NatureDeprecation, CompatFull, VerdictAdditive},

		// Widened: compatible under every policy.
		{NatureWidened, CompatBackward, VerdictCompatible},
		{NatureWidened, CompatForward, VerdictCompatible},
		{NatureWidened, CompatFull, VerdictCompatible},
	}
	for _, c := range cases {
		got, ok := classify(c.nature, c.compat)
		if !ok {
			t.Errorf("classify(%q,%q) unexpectedly unrecognized", c.nature, c.compat)
			continue
		}
		if got != c.want {
			t.Errorf("classify(%q,%q) = %v, want %v", c.nature, c.compat, got, c.want)
		}
	}
}

// TestClassifyUnknownNatureFailsClosed pins the fail-closed default: a nature Grip
// does not recognize must return ok=false (never a silent VerdictCompatible pass).
// Removing the `default` guard in classify() flips this to a false pass.
func TestClassifyUnknownNatureFailsClosed(t *testing.T) {
	for _, n := range []Nature{"", "teleported", "mutated-field"} {
		if _, ok := classify(n, CompatBackward); ok {
			t.Errorf("classify(%q) reported recognized; an unknown nature must fail closed", n)
		}
	}
}
