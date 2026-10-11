package portalapi

import (
	"testing"

	"github.com/fattymango/lazycake/internal/version"
)

func TestVersionInfoStates(t *testing.T) {
	old := latestAgentGateway
	defer func() { latestAgentGateway = old }()
	latestAgentGateway = "v0.4.4"
	for _, tc := range []struct{ reported, state string }{
		{"v0.4.4", "current"},
		{"v0.4.10", "current"}, // newer than the last agent/gateway change: not "outdated"
		{"v0.4.3", "outdated"},
		{"v0.3.13", "outdated"},
		{"dev", "current"}, // a development build has no release number: nothing to say
		{"", "unknown"},    // predates version reporting
	} {
		got := versionInfo(tc.reported)
		if got.UpdateState != tc.state || got.LatestVersion != "v0.4.4" || got.Version != tc.reported {
			t.Errorf("versionInfo(%q) = %+v, want state %s", tc.reported, got, tc.state)
		}
	}
}

// The point of keeping the two apart: a coordinator release that did not touch the agent or gateway must not make them outdated.
func TestCoordinatorReleaseDoesNotMakeAgentsOutdated(t *testing.T) {
	old := version.Version
	defer func() { version.Version = old }()
	for _, coordinator := range []string{"v0.4.4", "v0.4.10", "v0.9.0", "v1.0.0"} {
		version.Version = coordinator
		if got := versionInfo("v0.4.4"); got.UpdateState != "current" {
			t.Errorf("with the coordinator at %s an unchanged agent reads %q, want current", coordinator, got.UpdateState)
		}
	}
}

// The default tracks the constant that the guard test in internal/version keeps honest.
func TestLatestAgentGatewayDefaultsToTheRelease(t *testing.T) {
	if latestAgentGateway != version.AgentGatewayRelease {
		t.Fatalf("latestAgentGateway = %q, want %q", latestAgentGateway, version.AgentGatewayRelease)
	}
}
