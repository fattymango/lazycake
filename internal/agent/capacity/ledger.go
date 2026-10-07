// Package capacity is the agent's local admission control: the
// coordinator's view of a node's free capacity is always slightly stale
// (PLAN.md "Agent and host"), so the agent keeps its own ledger and is the
// final authority on whether a dispatched task actually fits.
package capacity

import (
	"fmt"
	"sync"
)

// Resources is a bundle of the three provisioned dimensions a task or an
// offer is measured in.
type Resources struct {
	Cores    float64
	MemoryMB int
	DiskMB   int
	// NetworkMbps is the tunnel bandwidth: what is offered, or (for a machine's physical
	// capacity) its fastest physical link, 0 when that can't be read. It is enforced as a rate
	// limit on tunnel traffic, not by admission, so Fits and the ledger ignore it.
	NetworkMbps float64
}

// Fits reports whether want fits within have on every dimension.
func (have Resources) Fits(want Resources) bool {
	return want.Cores <= have.Cores && want.MemoryMB <= have.MemoryMB && want.DiskMB <= have.DiskMB
}

func (a Resources) sub(b Resources) Resources {
	return Resources{Cores: a.Cores - b.Cores, MemoryMB: a.MemoryMB - b.MemoryMB, DiskMB: a.DiskMB - b.DiskMB}
}

func (a Resources) add(b Resources) Resources {
	return Resources{Cores: a.Cores + b.Cores, MemoryMB: a.MemoryMB + b.MemoryMB, DiskMB: a.DiskMB + b.DiskMB}
}

// ErrDoesNotFit is returned by Admit when a request exceeds free capacity.
type ErrDoesNotFit struct {
	Want, Free Resources
}

func (e ErrDoesNotFit) Error() string {
	return fmt.Sprintf("request %+v does not fit free capacity %+v", e.Want, e.Free)
}

// Ledger tracks one node's offer and what is currently allocated to
// running tasks. It is the single source of truth the agent's own
// admission control consults before ever starting a container - the
// coordinator's dispatch is only a reservation request, not a guarantee.
type Ledger struct {
	mu        sync.Mutex
	offer     Resources
	allocated map[string]Resources // keyed by task ID, so Release is exact and idempotent-safe
}

// NewLedger returns a Ledger starting with offer and nothing allocated.
func NewLedger(offer Resources) *Ledger {
	return &Ledger{offer: offer, allocated: make(map[string]Resources)}
}

// Admit reserves want for taskID if it fits in current free capacity, or
// returns ErrDoesNotFit without reserving anything. Reservation and the
// fit check happen under one lock, so concurrent Admit calls can never
// both succeed for a request that only fits once.
func (l *Ledger) Admit(taskID string, want Resources) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.allocated[taskID]; exists {
		return fmt.Errorf("capacity: task %s already admitted", taskID)
	}

	free := l.freeLocked()
	if !free.Fits(want) {
		return ErrDoesNotFit{Want: want, Free: free}
	}
	l.allocated[taskID] = want
	return nil
}

// Release frees taskID's reservation. Releasing an unknown or
// already-released task is a no-op, since exit-path cleanup may race a
// fence or a duplicate report.
func (l *Ledger) Release(taskID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.allocated, taskID)
}

// Free returns current free capacity.
func (l *Ledger) Free() Resources {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.freeLocked()
}

func (l *Ledger) freeLocked() Resources {
	used := Resources{}
	for _, r := range l.allocated {
		used = used.add(r)
	}
	return l.offer.sub(used)
}

// TaskIDs returns every currently admitted task ID, for re-announcing on
// reconnect (PLAN.md "Task adoption on reconnect").
func (l *Ledger) TaskIDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]string, 0, len(l.allocated))
	for id := range l.allocated {
		ids = append(ids, id)
	}
	return ids
}

// Offer returns the currently configured offer.
func (l *Ledger) Offer() Resources {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.offer
}

// SetOffer updates the offer. Per PLAN.md "lowering an offer mid-flight
// drains rather than evicts": this never touches existing allocations,
// even if the new offer is now lower than what's already allocated - Free
// will simply report negative-clamped-to-zero headroom until enough tasks
// finish, so new admissions stop but nothing running is killed.
func (l *Ledger) SetOffer(offer Resources) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.offer = offer
}
