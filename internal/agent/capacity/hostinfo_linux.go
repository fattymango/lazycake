//go:build linux

package capacity

import (
	"bufio"
	"fmt"
	"os"
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
		Cores:    float64(goruntime.NumCPU()),
		MemoryMB: memMB,
		DiskMB:   diskMB,
	}, nil
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
