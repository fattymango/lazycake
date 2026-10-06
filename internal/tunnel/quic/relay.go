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
	"encoding/hex"
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

// connConfig keeps the underlying QUIC connection itself alive across long
// stretches with no relayed stream open (e.g. a gateway just sitting there
// waiting for work) - without an explicit KeepAlivePeriod below quic-go's
// default 30s MaxIdleTimeout, the connection times out from inactivity even
// though nothing is actually wrong, and the gateway has to redial.
// A short idle timeout matters because a coordinator that restarts keeps no
// state a client could be reset against: without a CONNECTION_CLOSE the
// client only notices once the idle timeout lapses, and a gateway that
// takes that long to notice is unreachable for that whole time.
var connConfig = &quicgo.Config{KeepAlivePeriod: 5 * time.Second, MaxIdleTimeout: 15 * time.Second}

// Authenticator validates a bearer token and returns the account it
// belongs to. The coordinator's store.Store satisfies a trivial adapter
// of this; tunnel/quic never imports coordinator packages directly.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (accountID string, err error)
}

// GatewayRegistry lets the relay verify a connecting gateway's identity
// and record its published Noise key, without importing coordinator
// packages directly - the coordinator wires its own store.Store-backed
// implementation in.
type GatewayRegistry interface {
	// OwnsGateway reports whether gatewayID belongs to accountID, so a
	// valid token for one account can't register as someone else's
	// gateway ID.
	OwnsGateway(ctx context.Context, accountID, gatewayID string) (bool, error)
	// SetGatewayConnected records connectedness and, when connected is
	// true, the Noise public key the gateway just published.
	SetGatewayConnected(ctx context.Context, gatewayID string, connected bool, noisePubkey []byte) error
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
	Auth     Authenticator
	Gateways GatewayRegistry // may be nil: gateway ownership/pubkey persistence then just doesn't happen
	Log      *slog.Logger

	// OnStreamClosed, if set, is called once per relayed stream after it
	// finishes, with final byte counts.
	OnStreamClosed func(StreamStats)

	mu       sync.Mutex
	gateways map[string]*quicgo.Conn
	conns    map[*quicgo.Conn]struct{} // every live connection, so shutdown can close them
}

// Serve accepts connections on addr until ctx is cancelled.
func (r *Relay) Serve(ctx context.Context, addr string) error {
	r.mu.Lock()
	if r.gateways == nil {
		r.gateways = make(map[string]*quicgo.Conn)
	}
	if r.conns == nil {
		r.conns = make(map[*quicgo.Conn]struct{})
	}
	r.mu.Unlock()
	tlsConf, err := selfSignedServerTLSConfig()
	if err != nil {
		return fmt.Errorf("building TLS config: %w", err)
	}
	ln, err := quicgo.ListenAddr(addr, tlsConf, connConfig)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
		r.closeAll()
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

// closeAll tells every connected agent and gateway the relay is going away.
// Without it they only find out after the idle timeout, which for a gateway
// means being unreachable for that long after a coordinator restart.
func (r *Relay) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.conns {
		c.CloseWithError(0, "relay shutting down")
	}
}

func (r *Relay) handleConn(ctx context.Context, conn *quicgo.Conn) {
	r.mu.Lock()
	r.conns[conn] = struct{}{}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.conns, conn)
		r.mu.Unlock()
	}()
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
	accountID, err := r.Auth.Authenticate(ctx, cf.Token)
	if err != nil {
		r.Log.Warn("relay auth failed", "role", cf.Role, "error", err)
		conn.CloseWithError(1, "authentication failed")
		return
	}

	switch cf.Role {
	case "gateway":
		if r.Gateways != nil {
			owns, err := r.Gateways.OwnsGateway(ctx, accountID, cf.GatewayID)
			if err != nil {
				r.Log.Warn("checking gateway ownership", "gateway_id", cf.GatewayID, "error", err)
				conn.CloseWithError(1, "internal error")
				return
			}
			if !owns {
				r.Log.Warn("gateway id does not belong to this account", "gateway_id", cf.GatewayID, "account_id", accountID)
				conn.CloseWithError(1, "gateway id does not belong to this account")
				return
			}
			pubkey, err := hex.DecodeString(cf.NoisePubkey)
			if err != nil {
				r.Log.Warn("decoding gateway noise pubkey", "gateway_id", cf.GatewayID, "error", err)
				conn.CloseWithError(1, "malformed noise pubkey")
				return
			}
			if err := r.Gateways.SetGatewayConnected(ctx, cf.GatewayID, true, pubkey); err != nil {
				r.Log.Warn("recording gateway connection", "gateway_id", cf.GatewayID, "error", err)
			}
		}
		r.registerGateway(cf.GatewayID, conn)
		defer func() {
			// Only the connection that is still the registered one may mark
			// the gateway disconnected. A gateway that reconnects before its
			// old connection times out has already replaced it, and the old
			// one's late cleanup would otherwise flip a live gateway to
			// "disconnected".
			if r.unregisterGateway(cf.GatewayID, conn) && r.Gateways != nil {
				if err := r.Gateways.SetGatewayConnected(context.Background(), cf.GatewayID, false, nil); err != nil {
					r.Log.Warn("recording gateway disconnection", "gateway_id", cf.GatewayID, "error", err)
				}
			}
		}()
		<-conn.Context().Done()
	case "agent":
		r.serveAgent(ctx, conn)
	default:
		r.Log.Warn("unknown role in control frame", "role", cf.Role)
	}
}

func (r *Relay) registerGateway(id string, conn *quicgo.Conn) {
	r.mu.Lock()
	old := r.gateways[id]
	r.gateways[id] = conn
	r.mu.Unlock()
	if old != nil && old != conn {
		old.CloseWithError(0, "superseded by a newer connection from the same gateway")
	}
}

// unregisterGateway removes conn and reports whether it was still the
// registered connection for id (false if a newer one replaced it).
func (r *Relay) unregisterGateway(id string, conn *quicgo.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gateways[id] == conn {
		delete(r.gateways, id)
		return true
	}
	return false
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
