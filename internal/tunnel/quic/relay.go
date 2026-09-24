// Package quic implements the outer transport described in PLAN.md
// "Transport": QUIC, relayed through the coordinator, carrying whatever
// the inner Noise session (internal/tunnel/noise) already encrypted. The
// relay pumps bytes between an agent's stream and a gateway's stream
// without ever decoding what's inside them - only the small framing
// header at the start of each stream (which gateway, which task) is ever
// read here.
package quic

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	quicgo "github.com/quic-go/quic-go"
)

// streamIdleTimeout closes a relayed stream if neither side sends
// anything for this long, per IMPLEMENTATION.md task 2.3.
const streamIdleTimeout = 30 * time.Second

// Authenticator validates a bearer token and returns the account it
// belongs to. The coordinator's store.Store satisfies a trivial adapter
// of this; tunnel/quic never imports coordinator packages directly.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (accountID string, err error)
}

// StreamStats is reported once a relayed stream closes, for the
// three-point byte reconciliation phase 4 builds on top of this.
type StreamStats struct {
	GatewayID      string
	TaskID         string
	BytesAgentToGW int64
	BytesGWToAgent int64
}

// Relay is the coordinator's QUIC endpoint: agents and gateways both dial
// it, and it pumps bytes between them.
type Relay struct {
	Auth Authenticator
	Log  *slog.Logger

	// OnStreamClosed, if set, is called once per relayed stream after it
	// finishes, with final byte counts.
	OnStreamClosed func(StreamStats)

	mu       sync.Mutex
	gateways map[string]*quicgo.Conn
}

// Serve accepts connections on addr until ctx is cancelled.
func (r *Relay) Serve(ctx context.Context, addr string) error {
	if r.gateways == nil {
		r.gateways = make(map[string]*quicgo.Conn)
	}
	tlsConf, err := selfSignedServerTLSConfig()
	if err != nil {
		return fmt.Errorf("building TLS config: %w", err)
	}
	ln, err := quicgo.ListenAddr(addr, tlsConf, nil)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accepting connection: %w", err)
		}
		go r.handleConn(ctx, conn)
	}
}

func (r *Relay) handleConn(ctx context.Context, conn *quicgo.Conn) {
	controlStream, err := conn.AcceptStream(ctx)
	if err != nil {
		r.Log.Warn("accepting control stream", "error", err)
		return
	}
	cf, err := readControlFrame(controlStream)
	if err != nil {
		r.Log.Warn("reading control frame", "error", err)
		return
	}
	if _, err := r.Auth.Authenticate(ctx, cf.Token); err != nil {
		r.Log.Warn("relay auth failed", "role", cf.Role, "error", err)
		conn.CloseWithError(1, "authentication failed")
		return
	}

	switch cf.Role {
	case "gateway":
		r.registerGateway(cf.GatewayID, conn)
		defer r.unregisterGateway(cf.GatewayID, conn)
		<-conn.Context().Done()
	case "agent":
		r.serveAgent(ctx, conn)
	default:
		r.Log.Warn("unknown role in control frame", "role", cf.Role)
	}
}

func (r *Relay) registerGateway(id string, conn *quicgo.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gateways[id] = conn
}

func (r *Relay) unregisterGateway(id string, conn *quicgo.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gateways[id] == conn {
		delete(r.gateways, id)
	}
}

func (r *Relay) serveAgent(ctx context.Context, agentConn *quicgo.Conn) {
	for {
		stream, err := agentConn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go r.relayStream(stream)
	}
}

func (r *Relay) relayStream(agentStream *quicgo.Stream) {
	hdr, err := readStreamHeader(agentStream)
	if err != nil {
		r.Log.Warn("reading stream header", "error", err)
		agentStream.CancelWrite(1)
		return
	}

	r.mu.Lock()
	gwConn, ok := r.gateways[hdr.GatewayID]
	r.mu.Unlock()
	if !ok {
		r.Log.Warn("relaying stream: gateway not connected", "gateway_id", hdr.GatewayID, "task_id", hdr.TaskID)
		agentStream.CancelWrite(2)
		return
	}

	gwStream, err := gwConn.OpenStreamSync(agentStream.Context())
	if err != nil {
		r.Log.Warn("opening gateway stream", "gateway_id", hdr.GatewayID, "error", err)
		agentStream.CancelWrite(3)
		return
	}
	if err := writeGatewayStreamHeader(gwStream, gatewayStreamHeader{TaskID: hdr.TaskID}); err != nil {
		r.Log.Warn("writing gateway stream header", "error", err)
		gwStream.CancelWrite(4)
		agentStream.CancelWrite(4)
		return
	}

	stats := StreamStats{GatewayID: hdr.GatewayID, TaskID: hdr.TaskID}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		stats.BytesAgentToGW = pumpWithIdleTimeout(gwStream, agentStream)
		gwStream.Close()
	}()
	go func() {
		defer wg.Done()
		stats.BytesGWToAgent = pumpWithIdleTimeout(agentStream, gwStream)
		agentStream.Close()
	}()
	wg.Wait()

	if r.OnStreamClosed != nil {
		r.OnStreamClosed(stats)
	}
}

// idleDeadliner is the subset of *quicgo.Stream (and net.Conn) pumpWithIdleTimeout
// needs, so it isn't locked to the concrete quic-go type.
type idleDeadliner interface {
	io.Reader
	SetReadDeadline(time.Time) error
}

// pumpWithIdleTimeout copies src to dst, resetting src's read deadline on
// every successful read, and returns the number of bytes copied. A src
// that goes silent for longer than streamIdleTimeout ends the copy.
func pumpWithIdleTimeout(dst io.Writer, src idleDeadliner) int64 {
	var total int64
	buf := make([]byte, 32*1024)
	for {
		src.SetReadDeadline(time.Now().Add(streamIdleTimeout))
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total
			}
			total += int64(n)
		}
		if err != nil {
			return total
		}
	}
}
