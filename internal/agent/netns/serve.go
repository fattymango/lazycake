package netns

import (
	"context"
	"io"
	"log/slog"
	"net"

	quicgo "github.com/quic-go/quic-go"
	"golang.org/x/time/rate"

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

// serve runs the parent-side loops against sockets the netns child bound
// and handed over: DNS answers, and for each target, accept a TCP
// connection and relay it out through QUIC+Noise, enforcing egressCapBytes
// (<=0 for unlimited) across every connection the task opens. Runs until
// ctx is cancelled or the sockets are closed.
func serve(ctx context.Context, cfg Config, dnsConn *net.UDPConn, listeners []*net.TCPListener, relayConn *quicgo.Conn, egressCapBytes int64, onEgressExceeded func(), counters *TunnelCounters, bandwidth *rate.Limiter, log *slog.Logger) {
	limiter := newEgressLimiter(egressCapBytes, onEgressExceeded)
	go serveDNSLoop(dnsConn, cfg.Targets, log)
	for i, t := range cfg.Targets {
		go serveTargetLoop(ctx, listeners[i], t, cfg, relayConn, limiter, counters, bandwidth, log)
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

func serveTargetLoop(ctx context.Context, ln *net.TCPListener, target Target, cfg Config, relayConn *quicgo.Conn, limiter *egressLimiter, counters *TunnelCounters, bandwidth *rate.Limiter, log *slog.Logger) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleTaskConn(ctx, conn, target, cfg, relayConn, limiter, counters, bandwidth, log)
	}
}

func handleTaskConn(ctx context.Context, conn net.Conn, target Target, cfg Config, relayConn *quicgo.Conn, limiter *egressLimiter, counters *TunnelCounters, bandwidth *rate.Limiter, log *slog.Logger) {
	limiter.register(conn)
	defer limiter.unregister(conn)
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
	log.Info("sending task connection through gateway", "task_id", cfg.TaskID,
		"gateway_id", target.GatewayID, "hostname", target.Hostname, "port", target.Port)

	// Only container->gateway (egress) bytes count against the cap: this
	// is the customer's own data leaving the host, which is what
	// limits.egress_mb bounds (PLAN.md's task descriptor).
	var bytesToGateway, bytesToTask int64
	done := make(chan struct{}, 2)
	go func() {
		bytesToGateway, _ = io.Copy(limitedWriter{ctx, meterWriter{countingWriter{session, limiter}, &counters.ToGateway}, bandwidth}, conn)
		stream.Close()
		done <- struct{}{}
	}()
	go func() {
		bytesToTask, _ = io.Copy(limitedWriter{ctx, meterWriter{conn, &counters.ToTask}, bandwidth}, session)
		conn.Close()
		done <- struct{}{}
	}()
	<-done
	<-done
	log.Info("task connection through gateway closed", "task_id", cfg.TaskID,
		"gateway_id", target.GatewayID, "hostname", target.Hostname,
		"bytes_to_gateway", bytesToGateway, "bytes_to_task", bytesToTask)
}
