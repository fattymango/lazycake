package portalapi

import "github.com/fattymango/lazycake/internal/version"

// latestAgentGateway is the newest release in which the agent or gateway program changed (a variable only so tests can set it). It is
// deliberately NOT the coordinator's own version: the coordinator and the dashboard ship far more often than the programs on
// customers' machines, and an unchanged agent or gateway is not "outdated" because the dashboard got a new page.
var latestAgentGateway = version.AgentGatewayRelease

// versionView is what the portal shows about the release a machine or gateway runs: its own version, the newest release that
// changed agents and gateways (no outside lookup), and whether it is behind that. An agent or gateway that predates version
// reporting sends nothing: "unknown", which also deserves an update.
type versionView struct {
	Version       string `json:"version"`
	LatestVersion string `json:"latest_version"`
	// UpdateState is "current", "outdated" (older than the newest agent/gateway release) or "unknown" (it never reported a release number).
	UpdateState string `json:"update_state"`
}

func versionInfo(reported string) versionView {
	v := versionView{Version: reported, LatestVersion: latestAgentGateway, UpdateState: "current"}
	if reported == "" {
		v.UpdateState = "unknown"
		return v
	}
	if older, known := version.Older(reported, latestAgentGateway); known && older {
		v.UpdateState = "outdated"
	}
	return v
}
