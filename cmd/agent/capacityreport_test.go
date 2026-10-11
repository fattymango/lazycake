package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fattymango/lazycake/internal/agent/capacity"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

type captureSender struct {
	mu   sync.Mutex
	msgs []*lazycakev1.CapacityReport
}

func (c *captureSender) Send(m *lazycakev1.AgentMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cr := m.GetCapacity(); cr != nil {
		c.msgs = append(c.msgs, cr)
	}
}

func (c *captureSender) reports() []*lazycakev1.CapacityReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*lazycakev1.CapacityReport(nil), c.msgs...)
}

func TestFreeCapacityIsReportedAtOnceAndWhenItChanges(t *testing.T) {
	ledger := capacity.NewLedger(capacity.Resources{Cores: 4, MemoryMB: 4096, DiskMB: 10000, NetworkMbps: 100})
	var s captureSender
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reportCapacity(ctx, ledger, &s)

	deadline := time.Now().Add(3 * time.Second)
	for len(s.reports()) < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	first := s.reports()
	if len(first) != 1 {
		t.Fatalf("the first report must go out immediately, got %d", len(first))
	}
	if first[0].FreeCores != 4 || first[0].FreeMemoryMb != 4096 || first[0].Offer.GetCores() != 4 || first[0].Offer.GetNetworkMbps() != 100 {
		t.Fatalf("first report wrong: %+v", first[0])
	}

	// A task takes a core and 1 GB: within a poll interval the coordinator hears about it.
	if err := ledger.Admit("tsk_1", capacity.Resources{Cores: 1, MemoryMB: 1024}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2*capacityPollEvery + time.Second)
	for len(s.reports()) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := s.reports()
	if len(got) < 2 || got[1].FreeCores != 3 || got[1].FreeMemoryMb != 3072 {
		t.Fatalf("a change in free capacity must be reported: %+v", got)
	}

	// Nothing changes: nothing more is sent (until the periodic refresh).
	n := len(s.reports())
	time.Sleep(capacityPollEvery + 500*time.Millisecond)
	if len(s.reports()) != n {
		t.Fatalf("an unchanged machine must not chatter: %d -> %d reports", n, len(s.reports()))
	}
}
