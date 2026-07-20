package toolversion

import (
	"fmt"
	"testing"
)

// grip:test behavior=semver-precedence contract
func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"16.3.0", "16.2.9", 1}, {"16.3.0", "16.3.0", 0}, {"16.3.0-rc.1", "16.3.0", -1},
		{"16.3.0-rc.2", "16.3.0-rc.10", -1}, {"v16.3", "16.3.0", 0}, {"1.0.0-alpha", "1.0.0-alpha.1", -1},
	}
	for _, tc := range cases {
		a, _ := Parse(tc.a)
		b, _ := Parse(tc.b)
		if got := Compare(a, b); got != tc.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func ExampleCompare() {
	older, _ := Parse("1.2.3")
	newer, _ := Parse("1.3.0")
	fmt.Println(Compare(older, newer))
	// Output: -1
}

func TestParseRejectsNonVersions(t *testing.T) {
	for _, raw := range []string{"", "resolved-at-runtime", "1.2.3-", "01.2.3", "1.2.3-01"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", raw)
		}
	}
}
