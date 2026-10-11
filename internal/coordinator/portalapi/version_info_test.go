package portalapi

import (
	"testing"

	"github.com/fattymango/lazycake/internal/version"
)

func TestVersionInfoStates(t *testing.T) {
	old := version.Version
	defer func() { version.Version = old }()
	version.Version = "v0.3.13-2-gabc123"
	for _, tc := range []struct{ reported, state string }{
		{"v0.3.13", "current"},
		{"v0.3.14", "current"}, // newer than the coordinator: not "outdated"
		{"v0.3.12", "outdated"},
		{"v0.2.0", "outdated"},
		{"dev", "current"}, // a development build has no release number: nothing to say
		{"", "unknown"},    // predates version reporting
	} {
		got := versionInfo(tc.reported)
		if got.UpdateState != tc.state || got.LatestVersion != "v0.3.13-2-gabc123" || got.Version != tc.reported {
			t.Errorf("versionInfo(%q) = %+v, want state %s", tc.reported, got, tc.state)
		}
	}
}
