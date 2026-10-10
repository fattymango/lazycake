//go:build linux

package conn

import (
	"context"
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

// The dialed socket must carry the dead-connection timeout: that is what turns a silently dead connection into an
// error the gRPC channel can react to.
func TestDialerSetsTheDeadSocketTimeoutAndKeepalive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			defer c.Close()
		}
	}()

	c, err := Dialer(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var userTimeout, keepalive int
	_ = raw.Control(func(fd uintptr) {
		userTimeout, _ = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT)
		keepalive, _ = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_KEEPALIVE)
	})
	if userTimeout != 30000 {
		t.Fatalf("TCP_USER_TIMEOUT = %d ms, want 30000", userTimeout)
	}
	if keepalive != 1 {
		t.Fatal("TCP keepalive must be on")
	}
}
