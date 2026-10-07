//go:build linux

package capacity

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"
)

// Physical reads the host's actual CPU count, RAM and free disk on the
// filesystem holding dataDir, for ClampOffer to cap a host's requested
// offer against (PLAN.md "Offer and admission").
func Physical(dataDir string) (Resources, error) {
	memMB, err := totalMemoryMB()
	if err != nil {
		return Resources{}, fmt.Errorf("reading total memory: %w", err)
	}
	diskMB, err := freeDiskMB(dataDir)
	if err != nil {
		return Resources{}, fmt.Errorf("reading free disk: %w", err)
	}
	return Resources{
		Cores:       float64(goruntime.NumCPU()),
		MemoryMB:    memMB,
		DiskMB:      diskMB,
		NetworkMbps: fastestLinkMbps("/sys/class/net"),
	}, nil
}

// fastestLinkMbps is the highest speed reported by a physical network interface (one with a
// backing device, which excludes loopback, bridges, veth and the like), or 0 if none reports one.
// Virtual machines and wifi adapters often report nothing; the caller then can't check a network
// offer against it. It sees only the interfaces of the network namespace it runs in, so an agent
// in a container needs host networking for this to mean anything.
func fastestLinkMbps(sysNet string) float64 {
	entries, err := os.ReadDir(sysNet)
	if err != nil {
		return 0
	}
	best := 0
	for _, e := range entries {
		dir := filepath.Join(sysNet, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "device")); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "speed"))
		if err != nil {
			continue
		}
		if mbps, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && mbps > best {
			best = mbps
		}
	}
	return float64(best)
}

func totalMemoryMB() (int, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			return kb / 1024, nil
		}
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

func freeDiskMB(path string) (int, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int(st.Bavail * uint64(st.Bsize) / (1024 * 1024)), nil
}
