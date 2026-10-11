// Package version carries the build-time version string shared by every binary.
package version

// Version is overridden at build time via -ldflags "-X ...version.Version=...".
var Version = "dev"

// AgentGatewayRelease is the newest release in which the agent or the gateway program itself changed. The dashboard calls a
// running agent or gateway "outdated" only when it is older than this, NOT when it is older than the coordinator: a
// dashboard-only coordinator release changes nothing for them, and must not make every machine ask for an update.
//
// Bump it (to the release you are cutting) in the same change as any edit to the agent or gateway code, and update
// recordedCodeHash in agentgateway_test.go; that test fails until you do, and lists exactly what it hashes.
const AgentGatewayRelease = "v0.4.4"
