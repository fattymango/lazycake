package main

import (
	"log/slog"
	"time"

	"google.golang.org/grpc"
)

// stopGRPC stops srv gracefully, but only waits timeout for it. GracefulStop
// blocks until every in-flight RPC ends, and an agent's Connect stream is
// deliberately long-lived: it ends only when the agent drops it, which a
// GOAWAY alone doesn't make it do. Unbounded, a restart sat there until
// systemd's 90s stop timeout killed the process - and the relay, already shut
// down, left every gateway unable to reconnect for that whole time.
func stopGRPC(srv *grpc.Server, timeout time.Duration, log *slog.Logger) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		log.Warn("gRPC graceful stop timed out, forcing it", "timeout", timeout.String())
		srv.Stop() // also unblocks the GracefulStop goroutine
		<-done
	}
}
