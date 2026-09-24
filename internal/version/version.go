// Package version carries the build-time version string shared by every binary.
package version

// Version is overridden at build time via -ldflags "-X ...version.Version=...".
var Version = "dev"
