package version

import "testing"

func TestBaseReadsTheReleaseUnderneath(t *testing.T) {
	for in, want := range map[string][3]int{
		"v0.3.13": {0, 3, 13}, "0.3.13": {0, 3, 13}, "v0.3.13-2-gabc123": {0, 3, 13}, "v1.20.3-dirty": {1, 20, 3}, " v2.0.0+meta ": {2, 0, 0},
	} {
		a, b, c, ok := Base(in)
		if !ok || [3]int{a, b, c} != want {
			t.Errorf("Base(%q) = %d.%d.%d ok=%v, want %v", in, a, b, c, ok, want)
		}
	}
	for _, in := range []string{"", "dev", "unknown", "v1.2", "v1.2.x", "v-1.2.3", "1.2.3.4"} {
		if _, _, _, ok := Base(in); ok {
			t.Errorf("Base(%q) must not be a release", in)
		}
	}
}

func TestOlder(t *testing.T) {
	for _, tc := range []struct {
		a, b         string
		older, known bool
	}{
		{"v0.3.12", "v0.3.13", true, true},
		{"v0.3.13", "v0.3.13", false, true},
		{"v0.3.13", "v0.3.12", false, true},
		{"v0.2.99", "v0.10.0", true, true}, // numeric, not lexical
		{"v0.9.0", "v0.10.0", true, true},
		{"v1.0.0", "v0.99.99", false, true},
		{"v0.3.13", "v0.3.13-4-gdeadbee", false, true}, // the coordinator is a few commits past the same release: not outdated
		{"", "v0.3.13", false, false},                  // an agent that predates version reporting
		{"dev", "v0.3.13", false, false},
		{"v0.3.13", "dev", false, false},
	} {
		older, known := Older(tc.a, tc.b)
		if older != tc.older || known != tc.known {
			t.Errorf("Older(%q, %q) = %v,%v want %v,%v", tc.a, tc.b, older, known, tc.older, tc.known)
		}
	}
}
