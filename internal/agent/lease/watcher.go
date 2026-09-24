// Package lease implements the agent's self-fencing: when it loses
// contact with the coordinator for too long, the agent itself notices and
// kills every running container, rather than relying on the coordinator
// to somehow reach in and stop them (PLAN.md "Lease and fencing"). See
// internal/agent/conn.Runner.FenceDeadline for how the deadline itself is
// computed and task 3.1's invariant for why the coordinator's own reclaim
// is guaranteed to fire no earlier than this.
package lease

import (
	"context"
	"log/slog"
	"time"

	"github.com/mkassab215/lazycake/internal/clock"
)

// Watcher polls a deadline function and fires OnFence exactly once each
// time the clock crosses it, resetting once the deadline moves back into
// the future (a reconnect extends it again).
type Watcher struct {
	// Deadline returns the current fence deadline; the zero time means
	// "no deadline yet" (e.g. before the first successful registration)
	// and never fences.
	Deadline func() time.Time
	Clock    clock.Clock
	Log      *slog.Logger
	// CheckInterval defaults to 1s if zero.
	CheckInterval time.Duration
	// OnFence is called (from the Watcher's own goroutine) the moment the
	// clock passes Deadline() without having been extended past it.
	OnFence func()
}

func (w *Watcher) now() time.Time {
	if w.Clock == nil {
		return time.Now()
	}
	return w.Clock.Now()
}

// Run polls until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	interval := w.CheckInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	fenced := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deadline := w.Deadline()
			if deadline.IsZero() {
				continue
			}
			now := w.now()
			switch {
			case now.After(deadline) && !fenced:
				fenced = true
				w.Log.Warn("lease deadline passed with no acknowledged heartbeat, self-fencing", "deadline", deadline, "now", now)
				if w.OnFence != nil {
					w.OnFence()
				}
			case !now.After(deadline):
				fenced = false // reconnected and extended past now again
			}
		}
	}
}
