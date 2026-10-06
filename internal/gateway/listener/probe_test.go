//go:build integration

package listener

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

type probeEnv struct {
	relay  *quic.Relay
	addr   string
	cancel context.CancelFunc
	ctx    context.Context
}

func startProbeEnv(t *testing.T) *probeEnv {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	addr := freeUDPAddr(t)
	relay := &quic.Relay{Auth: fakeAuth{}, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go relay.Serve(ctx, addr)
	time.Sleep(100 * time.Millisecond)
	return &probeEnv{relay: relay, addr: addr, cancel: cancel, ctx: ctx}
}

// connectGateway runs a real gateway listener against the relay, publishing the given services.
func (e *probeEnv) connectGateway(t *testing.T, id string, services map[string]string) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	kp, err := noise.GenerateKeypair()
	require.NoError(t, err)
	conn, err := quic.DialGateway(e.ctx, e.addr, "tok", id, kp.Public)
	require.NoError(t, err)
	l := &Listener{Conn: conn, Keypair: kp, Services: services, Log: log}
	go l.Run(e.ctx)
	time.Sleep(150 * time.Millisecond) // let the relay register it
}

func byName(p quic.GatewayProbe) map[string]quic.ServiceProbe {
	m := map[string]quic.ServiceProbe{}
	for _, s := range p.Services {
		m[s.Name] = s
	}
	return m
}

// Everything works: a connected gateway whose published services all answer.
func TestProbeAllServicesReachable(t *testing.T) {
	e := startProbeEnv(t)
	port := startFakeService(t)
	e.connectGateway(t, "gw_1", map[string]string{"db": fmt.Sprintf("127.0.0.1:%d", port)})

	got := e.relay.ProbeGateway(e.ctx, "gw_1", []string{"db"})
	require.True(t, got.Connected)
	require.True(t, got.Verified, "the gateway answered, so the service verdicts mean something")
	require.GreaterOrEqual(t, got.RTTMs, int64(0))
	require.Len(t, got.Services, 1)
	require.True(t, got.Services[0].OK, got.Services[0].Error)
	require.True(t, got.Services[0].Answered)
}

// The case this feature exists for: the gateway is fine but a service behind it isn't.
func TestProbeReportsEachServiceSeparately(t *testing.T) {
	e := startProbeEnv(t)
	up := startFakeService(t)
	// A port that was open a moment ago and now refuses connections.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	e.connectGateway(t, "gw_1", map[string]string{
		"up":   fmt.Sprintf("127.0.0.1:%d", up),
		"dead": fmt.Sprintf("127.0.0.1:%d", deadPort),
	})

	got := e.relay.ProbeGateway(e.ctx, "gw_1", []string{"up", "dead", "never-published"})
	require.True(t, got.Connected)
	require.True(t, got.Verified, "all three were answered, even though two said no")
	m := byName(got)

	require.True(t, m["up"].OK)
	require.False(t, m["dead"].OK)
	require.Contains(t, m["dead"].Error, "connection refused", "a refused port says what's wrong in plain words")
	require.False(t, m["never-published"].OK)
	require.Contains(t, m["never-published"].Error, "doesn't publish", "a service registered in the portal but missing from the gateway's own config is caught")
}

func TestProbeOfAGatewayThatIsNotConnected(t *testing.T) {
	e := startProbeEnv(t)
	got := e.relay.ProbeGateway(e.ctx, "gw_missing", []string{"db"})
	require.False(t, got.Connected)
	require.False(t, got.Verified)
	require.Empty(t, got.Services)
}

// A gateway that stopped (its relay session closed) must read as not connected, not hang.
func TestProbeAfterTheGatewayDisconnects(t *testing.T) {
	e := startProbeEnv(t)
	port := startFakeService(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	kp, _ := noise.GenerateKeypair()
	conn, err := quic.DialGateway(e.ctx, e.addr, "tok", "gw_1", kp.Public)
	require.NoError(t, err)
	l := &Listener{Conn: conn, Keypair: kp, Services: map[string]string{"db": fmt.Sprintf("127.0.0.1:%d", port)}, Log: log}
	go l.Run(e.ctx)
	time.Sleep(150 * time.Millisecond)
	require.True(t, e.relay.ProbeGateway(e.ctx, "gw_1", []string{"db"}).Connected)

	conn.CloseWithError(0, "gateway stopped")
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	got := e.relay.ProbeGateway(e.ctx, "gw_1", []string{"db"})
	require.False(t, got.Connected)
	require.Less(t, time.Since(start), 2*time.Second, "a gone gateway is reported at once, not after a timeout")
}

// An older gateway build doesn't know about probes: it never replies. That must
// show as "connected but not verified" - never as success.
func TestProbeOfAGatewayWithoutProbeSupportIsNotVerified(t *testing.T) {
	e := startProbeEnv(t)
	kp, _ := noise.GenerateKeypair()
	conn, err := quic.DialGateway(e.ctx, e.addr, "tok", "gw_old", kp.Public)
	require.NoError(t, err)
	// Accept streams and read the header but never answer, as an old gateway
	// (waiting for a Noise handshake that never comes) effectively does.
	go func() {
		for {
			s, _, err := quic.AcceptRelayedStream(e.ctx, conn)
			if err != nil {
				return
			}
			go io.Copy(io.Discard, s)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	got := e.relay.ProbeGateway(e.ctx, "gw_old", []string{"db"})
	require.True(t, got.Connected, "it is connected, and we can say so")
	require.False(t, got.Verified, "but the service can't be verified")
	require.Len(t, got.Services, 1)
	require.False(t, got.Services[0].Answered)
	require.Contains(t, got.Services[0].Error, "may need updating")
}

// Only the coordinator may probe. An agent opening a stream with a probe marker
// must be refused, or it could ask a customer's gateway which ports answer.
func TestAnAgentCannotProbeAGateway(t *testing.T) {
	e := startProbeEnv(t)
	port := startFakeService(t)
	e.connectGateway(t, "gw_1", map[string]string{"db": fmt.Sprintf("127.0.0.1:%d", port)})

	agent, err := quic.DialAgent(e.ctx, e.addr, "agent-tok")
	require.NoError(t, err)
	stream, err := quic.OpenRelayedStream(e.ctx, agent, "gw_1", quic.ProbePrefix+"db")
	require.NoError(t, err)
	_ = stream.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := stream.Read(buf)
	// Read can return bytes and an error together, so check both: no probe
	// reply may reach the agent, and the stream must have been refused by the
	// relay itself (code 5), not merely have timed out or ended.
	require.Zero(t, n, "a probe reply reached an agent: %q", buf[:n])
	var refused *quicgo.StreamError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, quicgo.StreamErrorCode(5), refused.ErrorCode, "the relay should refuse it explicitly")
}
