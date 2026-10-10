//go:build !linux

package conn

import (
	"context"
	"net"
	"time"
)

// Dialer on platforms without TCP_USER_TIMEOUT: a plain dial with keepalives (agents are Linux-only; this exists so
// the module builds elsewhere).
func Dialer(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 15 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}
