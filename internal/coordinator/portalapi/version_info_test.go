package portalapi

import (
	"testing"

	"github.com/fattymango/lazycake/internal/version"
)

func TestVersionInfoStates(t *testing.T) {
	for _, tc := range []struct{ reported, state string }{
		{"v0.4.4", "current"},
		{"v0.4.10", "current"}, // newer than the last change: not "outdated"
		{"v0.4.3", "outdated"},
		{"v0.3.13", "outdated"},
		{"dev", "current"}, // a development build has no release number: nothing to say
		{"", "unknown"},    // predates version reporting
	} {
		got := versionInfo(tc.reported, "v0.4.4")
		if got.UpdateState != tc.state || got.LatestVersion != "v0.4.4" || got.Version != tc.reported {
			t.Errorf("versionInfo(%q) = %+v, want state %s", tc.reported, got, tc.state)
		}
	}
}

// The point of keeping them apart: neither a coordinator release nor a change to the other program makes an unchanged one outdated.
func TestOnlyItsOwnProgramsReleaseCounts(t *testing.T) {
	oldV, oldA, oldG := version.Version, latestAgent, latestGateway
	defer func() { version.Version, latestAgent, latestGateway = oldV, oldA, oldG }()
	latestAgent, latestGateway = "v0.4.4", "v0.4.12" // the gateway changed in a later release than the agent
	for _, coordinator := range []string{"v0.4.4", "v0.4.12", "v0.9.0"} {
		version.Version = coordinator
		if got := versionInfo("v0.4.8", latestAgent); got.UpdateState != "current" {
			t.Errorf("coordinator at %s: an agent unchanged since v0.4.4 reads %q, want current", coordinator, got.UpdateState)
		}
		if got := versionInfo("v0.4.8", latestGateway); got.UpdateState != "outdated" {
			t.Errorf("coordinator at %s: a gateway behind its own newest release reads %q, want outdated", coordinator, got.UpdateState)
		}
	}
}

func TestDefaultsTrackTheReleaseConstants(t *testing.T) {
	if latestAgent != version.AgentRelease || latestGateway != version.GatewayRelease {
		t.Fatalf("latestAgent/latestGateway = %q/%q, want %q/%q", latestAgent, latestGateway, version.AgentRelease, version.GatewayRelease)
	}
}
