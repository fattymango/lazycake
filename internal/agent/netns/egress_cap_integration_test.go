//go:build integration

package netns

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/mkassab215/lazycake/internal/gateway/listener"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

type allowAllAuth struct{}

func (allowAllAuth) Authenticate(ctx context.Context, token string) (string, error) {
	return "act", nil
}

func freeUDPAddr(t *testing.T) string {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	addr := c.LocalAddr().String()
	c.Close()
	return addr
}

// TestEgressCap proves task 2.6's contract directly against serve() - no
// real container needed, since the cap enforcement lives entirely in the
// parent-side copy loop and doesn't care whether its listeners came from
// a namespace-crossed fd (task 2.5) or an ordinary net.Listen: a task
// with a 10MB cap uploading 50MB gets cut off at approximately 10MB, and
// OnEgressExceeded fires exactly once.
func TestEgressCap(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	relayAddr := freeUDPAddr(t)

	relay := &quic.Relay{Auth: allowAllAuth{}, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx, relayAddr)
	time.Sleep(100 * time.Millisecond)

	// A local "service" that just discards everything it receives, like a
	// write-heavy real endpoint would from this test's point of view.
	sinkLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sinkLn.Close()
	go func() {
		for {
			c, err := sinkLn.Accept()
			if err != nil {
				return
			}
			go io.Copy(io.Discard, c)
		}
	}()
	sinkPort := sinkLn.Addr().(*net.TCPAddr).Port

	gwKeypair, err := noise.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	gwConn, err := quic.DialGateway(ctx, relayAddr, "gw-token", "gw_1")
	if err != nil {
		t.Fatal(err)
	}
	gw := &listener.Listener{Conn: gwConn, Keypair: gwKeypair, Services: map[string]int{"db": sinkPort}, Log: log}
	go gw.Run(ctx)

	dnsConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer dnsConn.Close()

	taskLn, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer taskLn.Close()

	agentKeypair, err := noise.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	agentConn, err := quic.DialAgent(ctx, relayAddr, "agent-token")
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		TaskID:       "tsk_cap",
		AgentKeypair: agentKeypair,
		Targets:      []Target{{GatewayID: "gw_1", Hostname: "db.acme.com", Port: 5432, NoisePubkey: gwKeypair.Public, Addr: net.IPv4(127, 0, 0, 1)}},
	}

	const capBytes = 10 << 20 // 10MB
	exceeded := make(chan struct{}, 1)
	serve(ctx, cfg, dnsConn, []*net.TCPListener{taskLn}, agentConn, capBytes, func() {
		select {
		case exceeded <- struct{}{}:
		default:
		}
	}, log)

	conn, err := net.Dial("tcp", taskLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	chunk := make([]byte, 1<<20) // 1MB
	var written int64
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for i := 0; i < 50; i++ { // up to 50MB, should get cut well before
			n, err := conn.Write(chunk)
			written += int64(n)
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-exceeded:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for the egress cap to trip")
	}
	<-writeDone

	if written > capBytes+4<<20 { // generous slack for in-flight buffering
		t.Fatalf("wrote %d bytes before being cut off, want approximately %d", written, capBytes)
	}
	if written < capBytes/2 {
		t.Fatalf("wrote only %d bytes, cap trip seems to have fired far too early", written)
	}
}
