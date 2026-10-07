package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/gateway/listener"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// flushEvery is how often batched byte counts go to the coordinator. A task that opens hundreds of tiny
// connections a second used to cost the coordinator one database transaction per connection, which a small
// coordinator cannot keep up with (found by a production load test: heartbeats starved, agents' leases
// expired and they fenced their tasks). Now the cost is one report per task and service per interval,
// however many connections there were.
const flushEvery = 5 * time.Second

// reporter batches the byte-count deltas of forwarded connections and sends them to the coordinator once per
// flushEvery, one report per task and service. Nothing is dropped: a report that can't be delivered (the
// coordinator is restarting, a network blip) stays pending and is added to the next one, so a gap in
// connectivity costs a delay in the statistics rather than missing bytes.
type reporter struct {
	gatewayID string
	log       *slog.Logger
	send      func(*lazycakev1.ByteReport) error

	mu      sync.Mutex
	pending map[string]*batch // task|service -> unsent totals
}

type batch struct {
	task, service string
	toLocal       int64
	toTask        int64
	connections   int32 // connections that closed in this batch
}

func newReporter(gatewayID string, log *slog.Logger, send func(*lazycakev1.ByteReport) error) *reporter {
	return &reporter{gatewayID: gatewayID, log: log, send: send, pending: map[string]*batch{}}
}

// Report records one connection's delta. It never does I/O, so the forwarding path is never slowed by the
// coordinator.
func (rp *reporter) Report(r listener.ForwardReport) {
	key := r.TaskID + "|" + r.Service
	rp.mu.Lock()
	defer rp.mu.Unlock()
	b, ok := rp.pending[key]
	if !ok {
		b = &batch{task: r.TaskID, service: r.Service}
		rp.pending[key] = b
	}
	b.toLocal += r.ToLocal
	b.toTask += r.ToTask
	if r.Final {
		b.connections++
	}
}

// Flush sends everything pending, one report per task and service. What fails stays pending.
func (rp *reporter) Flush() {
	rp.mu.Lock()
	taken := rp.pending
	rp.pending = map[string]*batch{}
	rp.mu.Unlock()

	for key, b := range taken {
		if b.toLocal == 0 && b.toTask == 0 && b.connections == 0 {
			continue
		}
		err := rp.send(&lazycakev1.ByteReport{
			GatewayId: rp.gatewayID, TaskId: b.task, Service: b.service,
			BytesToLocal: b.toLocal, BytesToTask: b.toTask, Final: b.connections > 0, Connections: b.connections,
		})
		if err != nil {
			rp.log.Warn("reporting byte counts; will retry with the next batch", "task_id", b.task, "error", err)
			rp.mu.Lock()
			if again, ok := rp.pending[key]; ok { // new bytes arrived for the same key meanwhile
				b.toLocal += again.toLocal
				b.toTask += again.toTask
				b.connections += again.connections
			}
			rp.pending[key] = b
			rp.mu.Unlock()
		}
	}
}

// Run flushes on a timer until ctx ends, then once more so a clean shutdown doesn't lose the last interval.
func (rp *reporter) Run(ctx context.Context) {
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			rp.Flush()
			return
		case <-t.C:
			rp.Flush()
		}
	}
}
