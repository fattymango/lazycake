// Package usage samples what this machine and its tasks are using, for the
// heartbeat (task 8.15). It reports machine totals only, never anything about the
// host's own processes, and it is display-only: nothing here feeds billing or trust.
package usage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// MaxTasks caps how many tasks one sample describes, so a heartbeat stays small
// however much is running.
const MaxTasks = 32

// Task is one running task the sampler should read.
type Task struct {
	TaskID      string
	ContainerID string
	// Tunnel returns the task's cumulative tunnel bytes (to the gateway, to the task);
	// nil for a task with no tunnel.
	Tunnel func() (toGateway, toTask int64)
}

// Container is one reading of a container: cumulative CPU time and current memory.
type Container struct {
	CPUNanos    uint64
	MemoryBytes uint64
}

// Sampler takes readings. Create it with a data directory and the two sources;
// the zero value of the rest does the right thing on Linux.
type Sampler struct {
	// Tasks lists the running tasks; Stats reads one container.
	Tasks func() []Task
	Stats func(ctx context.Context, containerID string) (Container, error)
	// DataDir is the directory whose filesystem the disk figures describe.
	DataDir string

	// ProcRoot, Statfs and Now are overridable for tests.
	ProcRoot string
	Statfs   func(path string) (total, free uint64, err error)
	Now      func() time.Time

	mu       sync.Mutex
	prevAt   time.Time
	prevHost cpuTimes
	prevCPU  map[string]uint64 // task id -> cumulative CPU nanos at the previous sample
}

type cpuTimes struct{ busy, total uint64 }

func (s *Sampler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sampler) proc(name string) string {
	root := s.ProcRoot
	if root == "" {
		root = "/proc"
	}
	return filepath.Join(root, name)
}

// Sample reads the machine and every running task. A figure that can't be read is
// left at zero rather than failing the heartbeat that carries it. It returns nil
// where there is nothing useful to say (an operating system without /proc).
func (s *Sampler) Sample(ctx context.Context) *lazycakev1.UsageSample {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	out := &lazycakev1.UsageSample{}
	var elapsed time.Duration
	if !s.prevAt.IsZero() {
		elapsed = now.Sub(s.prevAt)
		out.IntervalMs = elapsed.Milliseconds()
	}

	if host, cpus, err := readCPU(s.proc("stat")); err == nil {
		out.HostCpuCount = int32(cpus)
		if s.prevHost.total > 0 && host.total > s.prevHost.total {
			out.HostCpuBusy = clamp01(float64(host.busy-s.prevHost.busy) / float64(host.total-s.prevHost.total))
		}
		s.prevHost = host
	} else {
		return nil
	}
	if total, avail, err := readMem(s.proc("meminfo")); err == nil {
		out.HostMemTotalBytes = int64(total)
		if avail <= total {
			out.HostMemUsedBytes = int64(total - avail)
		}
	}
	statfs := s.Statfs
	if statfs == nil {
		statfs = statfsOf
	}
	if s.DataDir != "" {
		if total, free, err := statfs(s.DataDir); err == nil && free <= total {
			out.DiskTotalBytes = int64(total)
			out.DiskUsedBytes = int64(total - free)
		}
	}

	seen := make(map[string]uint64)
	if s.Tasks != nil && s.Stats != nil {
		for _, t := range s.Tasks() {
			if len(out.Tasks) >= MaxTasks {
				break
			}
			c, err := s.Stats(ctx, t.ContainerID)
			if err != nil {
				continue // the container may have just exited
			}
			tu := &lazycakev1.TaskUsage{TaskId: t.TaskID, MemoryBytes: int64(c.MemoryBytes)}
			if prev, ok := s.prevCPU[t.TaskID]; ok && elapsed > 0 && c.CPUNanos >= prev {
				tu.CpuCores = float64(c.CPUNanos-prev) / float64(elapsed.Nanoseconds())
			}
			seen[t.TaskID] = c.CPUNanos
			if t.Tunnel != nil {
				tu.TunnelBytesToGateway, tu.TunnelBytesToTask = t.Tunnel()
			}
			out.Tasks = append(out.Tasks, tu)
		}
	}
	s.prevCPU = seen
	s.prevAt = now
	return out
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// readCPU parses the aggregate "cpu" line of /proc/stat (busy = everything but idle
// and iowait) and counts the per-core lines.
func readCPU(path string) (cpuTimes, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return cpuTimes{}, 0, err
	}
	defer f.Close()
	var t cpuTimes
	cpus := 0
	got := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			cpus++
			continue
		}
		var vals []uint64
		for _, v := range fields[1:] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return cpuTimes{}, 0, fmt.Errorf("parsing %s: %w", path, err)
			}
			vals = append(vals, n)
		}
		if len(vals) < 5 {
			return cpuTimes{}, 0, fmt.Errorf("%s: short cpu line", path)
		}
		// user nice system idle iowait irq softirq steal ...: guest time is already inside user.
		var total uint64
		for i, v := range vals {
			if i < 8 {
				total += v
			}
		}
		t = cpuTimes{busy: total - vals[3] - vals[4], total: total}
		got = true
	}
	if !got {
		return cpuTimes{}, 0, fmt.Errorf("%s: no cpu line", path)
	}
	return t, cpus, sc.Err()
}

// readMem returns MemTotal and MemAvailable in bytes.
func readMem(path string) (total, avail uint64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var haveTotal, haveAvail bool
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, haveTotal = n*1024, true
		case "MemAvailable:":
			avail, haveAvail = n*1024, true
		}
	}
	if !haveTotal || !haveAvail {
		return 0, 0, fmt.Errorf("%s: missing MemTotal or MemAvailable", path)
	}
	return total, avail, sc.Err()
}
