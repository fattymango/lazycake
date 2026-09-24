package quic

import (
	"context"
	"encoding/hex"
	"fmt"

	quicgo "github.com/quic-go/quic-go"
)

// DialAgent connects to the relay at addr as an agent and completes the
// control handshake. The returned Conn is then used with OpenRelayedStream
// for each container connection that needs a tunnel.
func DialAgent(ctx context.Context, addr, token string) (*quicgo.Conn, error) {
	return dial(ctx, addr, controlFrame{Role: "agent", Token: token})
}

// DialGateway connects to the relay at addr as gatewayID and completes the
// control handshake, publishing noisePubkey so the coordinator can record
// it (see Relay.OnGatewayRegistered). The returned Conn is then used with
// AcceptRelayedStream to receive incoming task connections.
func DialGateway(ctx context.Context, addr, token, gatewayID string, noisePubkey []byte) (*quicgo.Conn, error) {
	return dial(ctx, addr, controlFrame{
		Role: "gateway", Token: token, GatewayID: gatewayID,
		NoisePubkey: hex.EncodeToString(noisePubkey),
	})
}

func dial(ctx context.Context, addr string, cf controlFrame) (*quicgo.Conn, error) {
	conn, err := quicgo.DialAddr(ctx, addr, insecureClientTLSConfig(), nil)
	if err != nil {
		return nil, fmt.Errorf("dialing relay at %s: %w", addr, err)
	}
	controlStream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening control stream: %w", err)
	}
	if err := writeControlFrame(controlStream, cf); err != nil {
		return nil, fmt.Errorf("sending control frame: %w", err)
	}
	return conn, nil
}

// OpenRelayedStream opens a new stream toward gatewayID for taskID. The
// returned stream carries whatever the caller writes to it straight to
// the gateway's corresponding stream (in this project, always an
// already-Noise-encrypted byte stream - see internal/tunnel/noise).
func OpenRelayedStream(ctx context.Context, conn *quicgo.Conn, gatewayID, taskID string) (*quicgo.Stream, error) {
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening relayed stream: %w", err)
	}
	if err := writeStreamHeader(stream, streamHeader{GatewayID: gatewayID, TaskID: taskID}); err != nil {
		return nil, fmt.Errorf("sending stream header: %w", err)
	}
	return stream, nil
}

// AcceptRelayedStream blocks until the relay opens a new stream toward
// this gateway connection (one per agent-side container connection) and
// returns it along with the task ID it's for.
func AcceptRelayedStream(ctx context.Context, conn *quicgo.Conn) (*quicgo.Stream, string, error) {
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("accepting relayed stream: %w", err)
	}
	hdr, err := readGatewayStreamHeader(stream)
	if err != nil {
		return nil, "", fmt.Errorf("reading gateway stream header: %w", err)
	}
	return stream, hdr.TaskID, nil
}
