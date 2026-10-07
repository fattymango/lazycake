package capacity

import (
	"fmt"
	"strings"
)

// ValidateOffer rejects an offer that is bigger than what the machine really has. It returns every
// problem at once (so the provider fixes them in one go), phrased with the setting to change.
//
// The limits are the machine's actual totals: its CPU count, total RAM, free disk where the agent
// keeps its data, and its fastest physical network link. An offer is never silently shrunk to fit:
// a provider who asks for more than exists gets an error and the agent does not start.
func ValidateOffer(physical, requested Resources) error {
	var problems []string
	if requested.Cores > physical.Cores {
		problems = append(problems, fmt.Sprintf("LAZYCAKE_OFFER_CORES=%s is more than this machine has (%s CPU cores)", trimF(requested.Cores), trimF(physical.Cores)))
	}
	if requested.MemoryMB > physical.MemoryMB {
		problems = append(problems, fmt.Sprintf("LAZYCAKE_OFFER_MEMORY_MB=%d is more than this machine has (%d MB of memory)", requested.MemoryMB, physical.MemoryMB))
	}
	if requested.DiskMB > physical.DiskMB {
		problems = append(problems, fmt.Sprintf("LAZYCAKE_OFFER_DISK_MB=%d is more than the free disk space here (%d MB)", requested.DiskMB, physical.DiskMB))
	}
	// A link speed that can't be read (virtual machines and wifi often don't report one) can't be
	// checked, so it isn't: the offer is then enforced as a rate limit regardless.
	if physical.NetworkMbps > 0 && requested.NetworkMbps > physical.NetworkMbps {
		problems = append(problems, fmt.Sprintf("LAZYCAKE_OFFER_NETWORK_MBPS=%s is more than this machine's fastest network link (%s Mbps)", trimF(requested.NetworkMbps), trimF(physical.NetworkMbps)))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("the offer is bigger than this machine: %s. Lower the offer and start the agent again", strings.Join(problems, "; "))
}

func trimF(f float64) string { return fmt.Sprintf("%g", f) }
