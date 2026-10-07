package main

import (
	"fmt"
	"os"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/config"
)

// runCapacity prints what this machine has, as one line the dashboard's "Add a machine" page can read:
//
//	cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000
//
// network_mbps is 0 when the machine's link speed can't be read (a virtual machine or wifi adapter), in
// which case an offered network limit is still enforced but can't be checked against the hardware.
func runCapacity() error {
	dir := os.Getenv("LAZYCAKE_DATA_DIR")
	if dir == "" {
		dir = config.DefaultDataDir
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		// Free disk is measured where the agent keeps its data; fall back to the temp dir.
		dir = os.TempDir()
	}
	p, err := capacity.Physical(dir)
	if err != nil {
		return err
	}
	fmt.Printf("cores=%g memory_mb=%d disk_mb=%d network_mbps=%g\n", p.Cores, p.MemoryMB, p.DiskMB, p.NetworkMbps)
	return nil
}
