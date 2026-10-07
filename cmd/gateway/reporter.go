package main

import (
	"log/slog"
	"sync"

	"github.com/mkassab215/lazycake/internal/gateway/listener"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// reporter sends a connection's byte-count deltas to the coordinator. A report
// that can't be delivered (the coordinator is restarting, a network blip) is not
// dropped: its bytes are carried and added to the next report for the same task
// and service, so a gap in connectivity costs a delay in the statistics rather
// than missing bytes. A connection that closes during the outage and never
// reports again leaves its carry behind (statistics are "as reported by the
// gateway", a floor), which is bounded by the number of distinct task/service
// pairs.
type reporter struct {
	gatewayID string
	log       *slog.Logger
	send      func(*lazycakev1.ByteReport) error

	mu      sync.Mutex
	pending map[string]*lazycakev1.ByteReport // task|service -> unsent bytes
}

func newReporter(gatewayID string, log *slog.Logger, send func(*lazycakev1.ByteReport) error) *reporter {
	return &reporter{gatewayID: gatewayID, log: log, send: send, pending: map[string]*lazycakev1.ByteReport{}}
}

// Report sends r's delta plus anything carried for the same task and service.
func (rp *reporter) Report(r listener.ForwardReport) {
	key := r.TaskID + "|" + r.Service

	rp.mu.Lock()
	out := &lazycakev1.ByteReport{
		GatewayId: rp.gatewayID, TaskId: r.TaskID, Service: r.Service,
		BytesToLocal: r.ToLocal, BytesToTask: r.ToTask, Final: r.Final,
	}
	if carried, ok := rp.pending[key]; ok {
		out.BytesToLocal += carried.BytesToLocal
		out.BytesToTask += carried.BytesToTask
		out.Final = out.Final || carried.Final // a connection that closed during an outage still counts
		delete(rp.pending, key)
	}
	rp.mu.Unlock()

	if err := rp.send(out); err != nil {
		rp.log.Warn("reporting byte counts; will retry with the next report", "task_id", r.TaskID, "error", err)
		rp.mu.Lock()
		if again, ok := rp.pending[key]; ok { // another report for the same key raced us
			out.BytesToLocal += again.BytesToLocal
			out.BytesToTask += again.BytesToTask
			out.Final = out.Final || again.Final
		}
		rp.pending[key] = out
		rp.mu.Unlock()
	}
}
