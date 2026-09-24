//go:build !linux

// lcinit only ever runs as a container's PID 1 on Linux; this file exists
// solely so `go build`/`go vet` succeed when developing on a non-Linux
// host (see internal/agent/netns's fds_other.go for the same pattern).
package main

import "os"

var forwardedSignals = []os.Signal{os.Interrupt}
