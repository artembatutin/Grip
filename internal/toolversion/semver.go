// Package toolversion implements the small, deterministic SemVer subset Grip
// needs to enforce configured analyzer minimums without trusting lexical order.
package toolversion

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semantic version. Build metadata is retained nowhere
// because SemVer deliberately excludes it from precedence.
type Version struct {
	Major int
	Minor int
	Patch int
	Pre   []identifier
}

type identifier struct {
	raw     string
	numeric bool
	n       int
}

// Parse accepts v-prefixed semantic versions with one to three numeric core
// components. Tool CLIs commonly emit "16" or "16.3"; Grip normalizes omitted
// components to zero while still rejecting non-version labels such as "latest".
func Parse(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	core, pre, hasPre := s, "", false
	if i := strings.IndexByte(s, '-'); i >= 0 {
		core, pre, hasPre = s[:i], s[i+1:], true
	}
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return Version{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	vals := [3]int{}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("invalid semantic version %q", raw)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("invalid semantic version %q", raw)
		}
		vals[i] = n
	}
	v := Version{Major: vals[0], Minor: vals[1], Patch: vals[2]}
	if !hasPre {
		return v, nil
	}
	if pre == "" {
		return Version{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	for _, p := range strings.Split(pre, ".") {
		if p == "" {
			return Version{}, fmt.Errorf("invalid semantic version %q", raw)
		}
		id := identifier{raw: p}
		allDigits := true
		for _, r := range p {
			if !validIdentifierRune(r) {
				return Version{}, fmt.Errorf("invalid semantic version %q", raw)
			}
			if r < '0' || r > '9' {
				allDigits = false
			}
		}
		if allDigits {
			if len(p) > 1 && p[0] == '0' {
				return Version{}, fmt.Errorf("invalid semantic version %q", raw)
			}
			id.numeric = true
			id.n, _ = strconv.Atoi(p)
		}
		v.Pre = append(v.Pre, id)
	}
	return v, nil
}

func validIdentifierRune(r rune) bool {
	return r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// Compare returns -1, 0, or 1 according to SemVer precedence.
func Compare(a, b Version) int {
	for _, pair := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(a.Pre) == 0 && len(b.Pre) == 0 {
		return 0
	}
	if len(a.Pre) == 0 {
		return 1
	}
	if len(b.Pre) == 0 {
		return -1
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		x, y := a.Pre[i], b.Pre[i]
		if x.numeric && y.numeric {
			if x.n < y.n {
				return -1
			}
			if x.n > y.n {
				return 1
			}
			continue
		}
		if x.numeric != y.numeric {
			if x.numeric {
				return -1
			}
			return 1
		}
		if x.raw < y.raw {
			return -1
		}
		if x.raw > y.raw {
			return 1
		}
	}
	if len(a.Pre) < len(b.Pre) {
		return -1
	}
	if len(a.Pre) > len(b.Pre) {
		return 1
	}
	return 0
}
