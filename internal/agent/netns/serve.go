package netns

import (
	"context"
	"io"
	"log/slog"
	"net"

	quicgo "github.com/quic-go/quic-go"

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

// serve runs the parent-side loops against sockets the netns child bound
// and handed over: DNS answers, and for each target, accept a TCP
// connection and relay it out through QUIC+Noise. Runs until ctx is
// cancelled or the sockets are closed.
func serve(ctx context.Context, cfg Config, dnsConn *net.UDPConn, listeners []*net.TCPListener, relayConn *quicgo.Conn, log *slog.Logger) {
	go serveDNSLoop(dnsConn, cfg.Targets, log)
	for i, t := range cfg.Targets {
		go serveTargetLoop(ctx, listeners[i], t, cfg, relayConn, log)
	}
}

func serveDNSLoop(conn *net.UDPConn, targets []Target, log *slog.Logger) {
	buf := make([]byte, 512)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		resp, err := handleDNSQuery(targets, buf[:n])
		if err != nil {
			log.Warn("malformed dns query", "error", err)
			continue
		}
		if _, err := conn.WriteToUDP(resp, addr); err != nil {
			log.Warn("writing dns response", "error", err)
		}
	}
}

func serveTargetLoop(ctx context.Context, ln *net.TCPListener, target Target, cfg Config, relayConn *quicgo.Conn, log *slog.Logger) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleTaskConn(ctx, conn, target, cfg, relayConn, log)
	}
}

func handleTaskConn(ctx context.Context, conn net.Conn, target Target, cfg Config, relayConn *quicgo.Conn, log *slog.Logger) {
	defer conn.Close()

	stream, err := quic.OpenRelayedStream(ctx, relayConn, target.GatewayID, cfg.TaskID)
	if err != nil {
		log.Warn("opening relayed stream", "task_id", cfg.TaskID, "hostname", target.Hostname, "error", err)
		return
	}
	defer stream.Close()

	session, err := noise.DoInitiatorHandshake(stream, cfg.AgentKeypair, target.NoisePubkey)
	if err != nil {
		log.Warn("noise handshake", "task_id", cfg.TaskID, "hostname", target.Hostname, "error", err)
		return
	}
	if _, err := session.Write([]byte(serviceName(target.Hostname))); err != nil {
		log.Warn("sending service name", "task_id", cfg.TaskID, "hostname", target.Hostname, "error", err)
		return
	}

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(session, conn)
		stream.Close()
		done <- struct{}{}
	}()
	go func() {
		io.Copy(conn, session)
		conn.Close()
		done <- struct{}{}
	}()
	<-done
	<-done
}
