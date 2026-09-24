//go:build !linux

package netns

import (
	"fmt"
	"net"
	"os"
)

// This package's fd-passing (see fds.go) is Linux-only, like the rest of
// the agent (PLAN.md/IMPLEMENTATION.md both scope it to Linux hosts).
// These stubs exist so the module still compiles for development on other
// platforms.

func sendFDs(conn *net.UnixConn, files []*os.File) error {
	return fmt.Errorf("netns: fd passing is unsupported on this platform, lazycake agents are Linux-only")
}

func socketpairFDs() ([2]int, error) {
	return [2]int{}, fmt.Errorf("netns: fd passing is unsupported on this platform, lazycake agents are Linux-only")
}

func recvFDs(conn *net.UnixConn, n int) ([]*os.File, error) {
	return nil, fmt.Errorf("netns: fd passing is unsupported on this platform, lazycake agents are Linux-only")
}
