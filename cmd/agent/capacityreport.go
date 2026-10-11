package main

import (
	"context"
	"time"

	"github.com/fattymango/lazycake/internal/agent/capacity"
	lcexec "github.com/fattymango/lazycake/internal/agent/exec"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

// capacityPollEvery is how often the agent looks for a change in its free capacity; capacityRefreshEvery is how long it
// goes without saying anything before it repeats itself (so a coordinator that restarted learns it again).
const (
	capacityPollEvery    = 2 * time.Second
	capacityRefreshEvery = 30 * time.Second
)

// reportCapacity tells the coordinator what this machine has free, whenever that changes. Without it the coordinator
// only knows the offer, so it dispatched tasks the agent had no room for; the agent then rejected them and each
// rejection cost a cooldown (357 of them in one load test). Free capacity is display-and-placement information only:
// the agent's own ledger stays the final authority on whether a task fits.
func reportCapacity(ctx context.Context, ledger *capacity.Ledger, send lcexec.Sender) {
	var last capacity.Resources
	var lastSent time.Time
	sentOnce := false
	tick := time.NewTicker(capacityPollEvery)
	defer tick.Stop()
	for {
		free := ledger.Free()
		if !sentOnce || free != last || time.Since(lastSent) >= capacityRefreshEvery {
			offer := ledger.Offer()
			send.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Capacity{Capacity: &lazycakev1.CapacityReport{
				Offer:        &lazycakev1.Offer{Cores: offer.Cores, MemoryMb: int32(offer.MemoryMB), DiskMb: int32(offer.DiskMB), NetworkMbps: int32(offer.NetworkMbps)},
				FreeCores:    free.Cores,
				FreeMemoryMb: int32(free.MemoryMB),
				FreeDiskMb:   int32(free.DiskMB),
			}}})
			last, lastSent, sentOnce = free, time.Now(), true
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
