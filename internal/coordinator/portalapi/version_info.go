package portalapi

import "github.com/fattymango/lazycake/internal/version"

// The newest release in which each program changed (variables only so tests can set them). Deliberately NOT the coordinator's own
// version: the coordinator and the dashboard ship far more often than the programs on customers' machines, and an unchanged agent
// or gateway is not "outdated" because the dashboard got a new page or the other program changed.
var (
	latestAgent   = version.AgentRelease
	latestGateway = version.GatewayRelease
)

// versionView is what the portal shows about the release a machine or gateway runs: its own version, the newest release that
// changed that program (no outside lookup), and whether it is behind that. An agent or gateway that predates version
// reporting sends nothing: "unknown", which also deserves an update.
type versionView struct {
	Version       string `json:"version"`
	LatestVersion string `json:"latest_version"`
	// UpdateState is "current", "outdated" (older than the newest agent/gateway release) or "unknown" (it never reported a release number).
	UpdateState string `json:"update_state"`
}

func versionInfo(reported, latest string) versionView {
	v := versionView{Version: reported, LatestVersion: latest, UpdateState: "current"}
	if reported == "" {
		v.UpdateState = "unknown"
		return v
	}
	if older, known := version.Older(reported, latest); known && older {
		v.UpdateState = "outdated"
	}
	return v
}
