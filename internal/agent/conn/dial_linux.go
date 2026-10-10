//go:build linux

package conn

import (
	"context"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// deadSocketTimeout is how long written data may stay unacknowledged before the kernel declares the connection dead
// (TCP_USER_TIMEOUT). Without it a connection that died silently, for example when a laptop was suspended and woke up,
// lingers for about 15 minutes of retransmissions, and every new gRPC stream is opened on that same dead socket and
// never gets out (found when a laptop agent stayed offline after a suspend even though it noticed the silence).
const deadSocketTimeout = 30 * time.Second

// Dialer is the context dialer the agent's gRPC client uses: it dials normally, keeps the connection alive with TCP
// keepalives, and makes the kernel give up on unacknowledged data after deadSocketTimeout so the gRPC channel sees the
// failure and reconnects on a fresh socket.
func Dialer(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 15 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(deadSocketTimeout/time.Millisecond))
			}); err != nil {
				return err
			}
			return serr
		},
	}
	return d.DialContext(ctx, "tcp", addr)
}
