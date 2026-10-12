package tlscert

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// One port for http and https. The first byte of a TLS connection is 0x16 (a handshake record); an HTTP request starts with a method name. The
// listener hands out connections that look at that byte on first use, so the portal and the MCP server answer both on the same port (8080) and
// nothing that already talks plain HTTP breaks. The peek happens on the connection's own goroutine, never in Accept, so one slow client cannot
// hold up the others.

var peekTimeout = 10 * time.Second

// ErrPlaintextRefused is what a connection reports when plaintext is not allowed (Options.RequireTLS) and the client did not start TLS.
var ErrPlaintextRefused = errors.New("plaintext connection refused: this port requires TLS")

// Options changes what Listener allows.
type Options struct {
	// RequireTLS closes any connection that does not start a TLS handshake (the gRPC port once every agent and gateway is updated: their tokens
	// must never cross the network in clear text).
	RequireTLS bool
	// OnRefused, if set, is told the remote address of each refused plaintext connection.
	OnRefused func(remote string)
}

// Listener wraps inner so every accepted connection speaks TLS (with cfg) if the client starts a handshake and plain text otherwise.
func Listener(inner net.Listener, cfg *tls.Config) net.Listener {
	return ListenerWith(inner, cfg, Options{})
}

// ListenerWith is Listener with options.
func ListenerWith(inner net.Listener, cfg *tls.Config, opt Options) net.Listener {
	return &listener{Listener: inner, cfg: cfg, opt: opt}
}

type listener struct {
	net.Listener
	cfg *tls.Config
	opt Options
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &lazyConn{Conn: c, cfg: l.cfg, opt: l.opt}, nil
}

type lazyConn struct {
	net.Conn
	cfg   *tls.Config
	opt   Options
	once  sync.Once
	inner net.Conn
	tls   bool
	err   error

	mu           sync.Mutex
	readDeadline time.Time // what the server asked for, so the peek never loosens it
}

// SetDeadline and SetReadDeadline remember the caller's deadline: the first-byte peek below must not cancel the header or handshake timeouts the
// HTTP and gRPC servers set before their first read.
func (c *lazyConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return c.Conn.SetDeadline(t)
}

func (c *lazyConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return c.Conn.SetReadDeadline(t)
}

func (c *lazyConn) init() {
	c.once.Do(func() {
		c.mu.Lock()
		asked := c.readDeadline
		c.mu.Unlock()
		peek := time.Now().Add(peekTimeout)
		if !asked.IsZero() && asked.Before(peek) {
			peek = asked
		}
		_ = c.Conn.SetReadDeadline(peek)
		var first [1]byte
		if _, err := io.ReadFull(c.Conn, first[:]); err != nil {
			c.err = err
			return
		}
		_ = c.Conn.SetReadDeadline(asked)
		pc := &prefixConn{Conn: c.Conn, prefix: first[:]}
		if first[0] == 0x16 {
			c.inner, c.tls = tls.Server(pc, c.cfg), true
			return
		}
		if c.opt.RequireTLS {
			c.err = ErrPlaintextRefused
			if c.opt.OnRefused != nil {
				c.opt.OnRefused(c.Conn.RemoteAddr().String())
			}
			_ = c.Conn.Close()
			return
		}
		c.inner = pc
	})
}

func (c *lazyConn) Read(p []byte) (int, error) {
	c.init()
	if c.err != nil {
		return 0, c.err
	}
	return c.inner.Read(p)
}

func (c *lazyConn) Write(p []byte) (int, error) {
	c.init()
	if c.err != nil {
		return 0, c.err
	}
	return c.inner.Write(p)
}

func (c *lazyConn) Close() error {
	if c.inner != nil {
		return c.inner.Close()
	}
	return c.Conn.Close()
}

// prefixConn replays the byte that was peeked.
type prefixConn struct {
	net.Conn
	prefix []byte
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

type ctxKey struct{}

// ConnContext is for http.Server.ConnContext: it lets Wrap see which kind of connection a request came in on.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if lc, ok := c.(*lazyConn); ok {
		return context.WithValue(ctx, ctxKey{}, lc)
	}
	return ctx
}

// Wrap marks requests that arrived over TLS (r.TLS is set, as it is for a normal https server, so Secure cookies and https URLs work) and, with
// forceHTTPS, sends plain-HTTP requests to the same address over https once ready() says a certificate is in place.
func Wrap(next http.Handler, forceHTTPS bool, ready func() bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lc, ok := r.Context().Value(ctxKey{}).(*lazyConn); ok && lc.tls {
			r.TLS = &tls.ConnectionState{HandshakeComplete: true}
		} else if forceHTTPS && ok && ready() && !isLoopback(r.RemoteAddr) {
			http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopback keeps the server's own health checks (curl http://localhost:8080/) working while everyone else is sent to https.
func isLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
