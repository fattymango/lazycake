// Package version carries the build-time version string shared by every binary.
package version

// Version is overridden at build time via -ldflags "-X ...version.Version=...".
var Version = "dev"

// AgentRelease and GatewayRelease are the newest releases in which the agent, respectively the gateway, program itself changed. The
// dashboard calls a running agent or gateway "outdated" only when it is older than ITS OWN number here, never because the coordinator
// or the other program moved on: a dashboard-only release, or a gateway-only one, must not make every machine ask for an update.
//
// Bump the one whose code you edit (to the release you are cutting) in the same change, and re-record its hash in
// agentgateway_test.go; that test fails until you do, and lists exactly what each one hashes.
const (
	AgentRelease   = "v0.4.15"
	GatewayRelease = "v0.4.12"
)
