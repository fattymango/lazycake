package runtime

import (
	"testing"

	"github.com/docker/docker/api/types"
)

func TestStatsFromSubtractsReclaimableCache(t *testing.T) {
	var raw types.StatsJSON
	raw.CPUStats.CPUUsage.TotalUsage = 12_000_000_000
	raw.MemoryStats.Usage = 300 << 20
	raw.MemoryStats.Stats = map[string]uint64{"inactive_file": 100 << 20}
	got := statsFrom(raw)
	if got.CPUNanos != 12_000_000_000 || got.MemoryBytes != 200<<20 {
		t.Fatalf("got %+v, want 12s of CPU and 200 MiB", got)
	}

	// An engine that reports more cache than usage must not wrap around.
	raw.MemoryStats.Stats = map[string]uint64{"inactive_file": 400 << 20}
	if got := statsFrom(raw); got.MemoryBytes != 300<<20 {
		t.Fatalf("cache larger than usage: got %d", got.MemoryBytes)
	}
}
