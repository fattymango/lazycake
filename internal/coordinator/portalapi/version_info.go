package portalapi

import "github.com/fattymango/lazycake/internal/version"

// versionView is what the portal shows about the release a machine or gateway runs: its own version, the coordinator's (the
// newest release this installation knows of; no outside lookup), and whether it is behind. An agent or gateway that predates
// version reporting sends nothing: "unknown", which also deserves an update.
type versionView struct {
	Version       string `json:"version"`
	LatestVersion string `json:"latest_version"`
	// UpdateState is "current", "outdated" (older than the coordinator) or "unknown" (it never reported a release number).
	UpdateState string `json:"update_state"`
}

func versionInfo(reported string) versionView {
	v := versionView{Version: reported, LatestVersion: version.Version, UpdateState: "current"}
	if reported == "" {
		v.UpdateState = "unknown"
		return v
	}
	if older, known := version.Older(reported, version.Version); known && older {
		v.UpdateState = "outdated"
	}
	return v
}
