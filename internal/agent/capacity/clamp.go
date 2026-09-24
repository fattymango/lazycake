package capacity

// Reserved headroom the host keeps for itself regardless of what the offer
// slider says, per PLAN.md "Offer and admission": "cap at roughly 75% of
// physical cores, leave several GB of RAM and real disk headroom."
const (
	maxCoreFraction  = 0.75
	reservedMemoryMB = 2048
	reservedDiskMB   = 10240
)

// ClampOffer caps a host-requested offer against physical capacity, so a
// host who offers everything can't accidentally starve their own desktop.
func ClampOffer(physical, requested Resources) Resources {
	maxCores := physical.Cores * maxCoreFraction
	maxMemory := physical.MemoryMB - reservedMemoryMB
	maxDisk := physical.DiskMB - reservedDiskMB

	return Resources{
		Cores:    clampF(requested.Cores, 0, maxCores),
		MemoryMB: clampI(requested.MemoryMB, 0, maxMemory),
		DiskMB:   clampI(requested.DiskMB, 0, maxDisk),
	}
}

func clampF(v, min, max float64) float64 {
	if max < min {
		max = min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampI(v, min, max int) int {
	if max < min {
		max = min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
