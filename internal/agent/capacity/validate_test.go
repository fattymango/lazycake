package capacity

import (
	"strings"
	"testing"
)

func TestValidateOfferRejectsAnythingBiggerThanTheMachine(t *testing.T) {
	physical := Resources{Cores: 8, MemoryMB: 16384, DiskMB: 500000, NetworkMbps: 1000}

	// Exactly what the machine has is allowed: the limit is the real total, not a fraction of it.
	if err := ValidateOffer(physical, physical); err != nil {
		t.Fatalf("an offer equal to the machine must be accepted: %v", err)
	}
	if err := ValidateOffer(physical, Resources{Cores: 2, MemoryMB: 4096, DiskMB: 10000, NetworkMbps: 100}); err != nil {
		t.Fatalf("a modest offer must be accepted: %v", err)
	}

	for _, tc := range []struct {
		name string
		over Resources
		want string
	}{
		{"cores", Resources{Cores: 8.5, MemoryMB: 1, DiskMB: 1}, "LAZYCAKE_OFFER_CORES=8.5 is more than this machine has (8 CPU cores)"},
		{"memory", Resources{Cores: 1, MemoryMB: 16385, DiskMB: 1}, "LAZYCAKE_OFFER_MEMORY_MB=16385 is more than this machine has (16384 MB"},
		{"disk", Resources{Cores: 1, MemoryMB: 1, DiskMB: 500001}, "LAZYCAKE_OFFER_DISK_MB=500001 is more than the free disk space here (500000 MB)"}, // size unknown: falls back to free space
		{"network", Resources{Cores: 1, MemoryMB: 1, DiskMB: 1, NetworkMbps: 1001}, "LAZYCAKE_OFFER_NETWORK_MBPS=1001 is more than this machine's fastest network link (1000 Mbps)"},
	} {
		err := ValidateOffer(physical, tc.over)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want an error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestValidateOfferReportsEveryProblemAtOnce(t *testing.T) {
	err := ValidateOffer(Resources{Cores: 2, MemoryMB: 1000, DiskMB: 1000}, Resources{Cores: 64, MemoryMB: 99999, DiskMB: 99999})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, s := range []string{"OFFER_CORES=64", "OFFER_MEMORY_MB=99999", "OFFER_DISK_MB=99999"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the error should name every oversized setting; missing %s in: %v", s, err)
		}
	}
}

func TestAnUnreadableLinkSpeedIsNotCheckedButAKnownOneIs(t *testing.T) {
	// A VM or wifi adapter that doesn't report a speed: nothing to compare against.
	if err := ValidateOffer(Resources{Cores: 4, MemoryMB: 8000, DiskMB: 8000}, Resources{Cores: 1, MemoryMB: 1, DiskMB: 1, NetworkMbps: 5000}); err != nil {
		t.Fatalf("an unknown link speed must not reject the offer: %v", err)
	}
}

// The disk limit is the disk's size: an offer that exceeds today's free space but fits the disk is accepted (and warned about
// by the caller), so a few unrelated files appearing can't stop an agent that started fine yesterday.
func TestTheDiskLimitIsTheDisksSizeNotItsFreeSpace(t *testing.T) {
	physical := Resources{Cores: 4, MemoryMB: 8000, DiskMB: 56_000, DiskTotalMB: 100_000}
	if err := ValidateOffer(physical, Resources{Cores: 1, MemoryMB: 1, DiskMB: 57_000}); err != nil {
		t.Fatalf("an offer a little above the current free space but within the disk must be accepted: %v", err)
	}
	err := ValidateOffer(physical, Resources{Cores: 1, MemoryMB: 1, DiskMB: 100_001})
	if err == nil || !strings.Contains(err.Error(), "more than the size of the disk here (100000 MB)") {
		t.Fatalf("an offer bigger than the disk itself must be refused, got %v", err)
	}
}
