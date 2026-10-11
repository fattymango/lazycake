package version

import (
	"strconv"
	"strings"
)

// Base reads the release a version string is built on: "v0.3.13", "0.3.13", "v0.3.13-2-gabc123" (two commits after
// the tag) and "v0.3.13-dirty" all give 0.3.13. ok is false for anything that isn't a release ("dev", "", "unknown").
func Base(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var n [3]int
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return 0, 0, 0, false
		}
		n[i] = x
	}
	return n[0], n[1], n[2], true
}

// Older reports whether version a is an older release than b. known is false when either isn't a release, in which case
// nothing can be said (an agent that predates version reporting sends nothing, and a development build has no number).
func Older(a, b string) (older, known bool) {
	a1, a2, a3, okA := Base(a)
	b1, b2, b3, okB := Base(b)
	if !okA || !okB {
		return false, false
	}
	switch {
	case a1 != b1:
		return a1 < b1, true
	case a2 != b2:
		return a2 < b2, true
	default:
		return a3 < b3, true
	}
}
