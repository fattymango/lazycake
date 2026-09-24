package api

import (
	"fmt"
	"sync"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// Registry tracks currently connected agents and lets other coordinator
// components (the scheduler, cancellation, canaries) push a message to a
// specific node without knowing anything about gRPC streams.
type Registry struct {
	mu    sync.Mutex
	conns map[string]chan *lazycakev1.CoordinatorMessage
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{conns: make(map[string]chan *lazycakev1.CoordinatorMessage)}
}

// Add registers nodeID's outbound channel. It replaces any existing entry
// for the same node (a reconnect), closing neither channel itself - callers
// own their own channel lifecycle.
func (r *Registry) Add(nodeID string, send chan *lazycakev1.CoordinatorMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[nodeID] = send
}

// Remove drops nodeID's entry if it still points at send (a stale Remove
// from an old connection must not clobber a fresher one after reconnect).
func (r *Registry) Remove(nodeID string, send chan *lazycakev1.CoordinatorMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[nodeID] == send {
		delete(r.conns, nodeID)
	}
}

// Send delivers msg to nodeID's outbound channel without blocking forever;
// ErrNotConnected if the node has no live stream.
func (r *Registry) Send(nodeID string, msg *lazycakev1.CoordinatorMessage) error {
	r.mu.Lock()
	ch, ok := r.conns[nodeID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotConnected, nodeID)
	}
	select {
	case ch <- msg:
		return nil
	default:
		return fmt.Errorf("%w: %s send buffer full", ErrBackpressure, nodeID)
	}
}

// Connected reports whether nodeID currently has a live stream.
func (r *Registry) Connected(nodeID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.conns[nodeID]
	return ok
}

// ConnectedNodeIDs returns every currently connected node ID.
func (r *Registry) ConnectedNodeIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.conns))
	for id := range r.conns {
		ids = append(ids, id)
	}
	return ids
}
