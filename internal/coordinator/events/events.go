// Package events is the coordinator's tiny in-process pub/sub for
// IMPLEMENTATION.md task 6.1's SSE stream: task state changes, node
// connect/disconnect, and capacity changes, published by whichever
// coordinator package already knows about them (api, scheduler) and
// consumed by the HTTP handler that turns them into server-sent events.
// It has no dependency on api or scheduler - both depend on it, not the
// other way around - so publishing an event never risks an import cycle.
package events

import "sync"

// Event is one thing worth telling a live dashboard about. Fields beyond
// Type/At are populated as relevant to that type and omitted (empty)
// otherwise - kept as a single flat struct rather than a union of types so
// JSON-encoding one is trivial and the SSE wire format stays simple.
type Event struct {
	Type string `json:"type"` // "task_state" | "node_connected" | "node_disconnected" | "capacity"
	AtMS int64  `json:"at_ms"`

	// task_state
	TaskID string `json:"task_id,omitempty"`
	State  string `json:"state,omitempty"`

	// node_connected / node_disconnected / capacity
	NodeID string `json:"node_id,omitempty"`

	// capacity
	FreeCores    float64 `json:"free_cores,omitempty"`
	FreeMemoryMB int32   `json:"free_memory_mb,omitempty"`
	FreeDiskMB   int32   `json:"free_disk_mb,omitempty"`
}

// Bus fans one Publish out to every current Subscriber. A slow or gone
// subscriber never blocks a publisher - Publish drops the event for that
// one subscriber instead of waiting.
type Bus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewBus returns an empty Bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[chan Event]struct{})}
}

// Subscribe registers a new listener and returns its channel plus a func
// to unregister it. Always call the returned cancel func (typically via
// defer) once done, or the channel leaks.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish sends e to every current subscriber, non-blocking. A nil Bus is
// a valid no-op receiver, so every caller can hold a *Bus field that's
// simply unset in tests that don't care about the event stream.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // slow subscriber - drop rather than block the publisher
		}
	}
}
