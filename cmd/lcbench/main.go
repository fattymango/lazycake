// Command lcbench is a small public benchmark workload for checking that a
// LazyCake node really caps a task at the resources it was provisioned.
//
// Duration comes from the LCBENCH_DURATION env var ("60", "60s", "1m"), or a
// single arg ("60", "60s", "duration=60s"); default 60s. LCBENCH_WORKERS,
// LCBENCH_MEM_STEP_MB and LCBENCH_MEM_MAX_MB (or the matching --flags) tune
// the load. No args are needed when run as a task: the image's entrypoint
// is used when a task's args are empty.
//
// For the given duration it burns CPU on --workers goroutines (default: one
// per host core, deliberately more than a capped container is allowed) and
// allocates and touches --mem-step-mb of memory every second up to
// --mem-max-mb (default: stop at 90% of the cgroup's memory limit, or 1024
// if there is none). Once a second it prints what the container's cgroup
// says it is using next to the limits it was given, so a capped CPU shows
// up as "cpu_used" pinned at "cpu_limit" with nr_throttled climbing, and a
// capped memory as "mem_used" levelling off below "mem_limit".
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const cgroupDir = "/sys/fs/cgroup"

func main() {
	workers := flag.Int("workers", envInt("LCBENCH_WORKERS", runtime.NumCPU()), "CPU-burning goroutines")
	stepMB := flag.Int("mem-step-mb", envInt("LCBENCH_MEM_STEP_MB", 16), "MB allocated and touched per second")
	maxMB := flag.Int("mem-max-mb", envInt("LCBENCH_MEM_MAX_MB", 0), "stop allocating at this many MB (0 = 90% of cgroup limit, or 1024)")
	flag.Parse()

	durStr := os.Getenv("LCBENCH_DURATION")
	if flag.NArg() > 0 {
		durStr = strings.TrimPrefix(flag.Arg(0), "duration=")
	}
	if durStr == "" {
		durStr = "60s"
	}
	dur, err := parseDuration(durStr)
	if err != nil || dur < time.Second {
		fmt.Fprintf(os.Stderr, "lcbench: bad duration %q (use e.g. 60, 60s, 1m)\n", durStr)
		os.Exit(2)
	}
	secs := int(dur.Seconds())

	memLimit := readInt(cgroupDir + "/memory.max") // -1 if "max" or unreadable
	cpuLimit := cpuLimitCores()
	capMB := *maxMB
	if capMB == 0 {
		capMB = 1024
		if memLimit > 0 {
			capMB = int(float64(memLimit) * 0.9 / (1 << 20))
		}
	}

	fmt.Printf("lcbench start: duration=%ds workers=%d host_cores=%d cpu_limit=%s mem_limit=%s mem_cap=%dMB\n",
		secs, *workers, runtime.NumCPU(), fmtCores(cpuLimit), fmtMB(memLimit), capMB)

	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	var stop atomic.Bool
	var iters atomic.Uint64
	for i := 0; i < *workers; i++ {
		go func() {
			x := 1.0001
			var n uint64
			for !stop.Load() {
				for j := 0; j < 1_000_000; j++ {
					x = math.Sqrt(x*x+float64(j)) + 1
				}
				n++
				iters.Add(1)
			}
			_ = x
			_ = n
		}()
	}

	var held [][]byte
	allocMB := 0
	start := time.Now()
	prevCPU := cpuUsageUsec()
	prev := start

	// A plain sleep, not a Ticker: a throttled process wakes late, and a
	// Ticker then delivers a second tick immediately, giving a near-zero
	// sampling interval and a wildly wrong rate.
	for {
		time.Sleep(time.Second)
		if allocMB+*stepMB <= capMB {
			b := make([]byte, *stepMB<<20)
			for i := 0; i < len(b); i += 4096 {
				b[i] = 1
			}
			held = append(held, b)
			allocMB += *stepMB
		}
		// Read the counter and the clock together: the ticker's own timestamp
		// can be well before this point (allocating above, or a throttled
		// process waking late), which skews the rate.
		cpu, now := cpuUsageUsec(), time.Now()
		used := float64(cpu-prevCPU) / 1e6 / now.Sub(prev).Seconds()
		prevCPU, prev = cpu, now
		_, throttled := cpuStat()
		fmt.Printf("t=%3ds cpu_used=%.2f cores (limit %s) throttled_periods=%d | mem_used=%s (limit %s) allocated=%dMB | loops=%d\n",
			int(now.Sub(start).Seconds()+0.5), used, fmtCores(cpuLimit), throttled,
			fmtMB(readInt(cgroupDir+"/memory.current")), fmtMB(memLimit), allocMB, iters.Load())
		if !time.Now().Before(deadline) {
			break
		}
	}
	stop.Store(true)
	fmt.Printf("lcbench done: peak_allocated=%dMB total_loops=%d (kept %d chunks live)\n", allocMB, iters.Load(), len(held))
}

func readInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return -1
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// cpuLimitCores parses cgroup v2 cpu.max ("<quota> <period>" or "max <period>").
func cpuLimitCores() float64 {
	b, err := os.ReadFile(cgroupDir + "/cpu.max")
	if err != nil {
		return -1
	}
	f := strings.Fields(string(b))
	if len(f) != 2 || f[0] == "max" {
		return -1
	}
	q, e1 := strconv.ParseFloat(f[0], 64)
	p, e2 := strconv.ParseFloat(f[1], 64)
	if e1 != nil || e2 != nil || p == 0 {
		return -1
	}
	return q / p
}

func cpuStat() (usageUsec, throttledPeriods int64) {
	b, err := os.ReadFile(cgroupDir + "/cpu.stat")
	if err != nil {
		return -1, -1
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		v, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "usage_usec":
			usageUsec = v
		case "nr_throttled":
			throttledPeriods = v
		}
	}
	return
}

func cpuUsageUsec() int64 { u, _ := cpuStat(); return u }

func fmtCores(c float64) string {
	if c < 0 {
		return "none"
	}
	return fmt.Sprintf("%.2f", c)
}

func fmtMB(b int64) string {
	if b < 0 {
		return "none"
	}
	return fmt.Sprintf("%dMB", b>>20)
}

// parseDuration accepts a Go duration ("60s", "1m") or a bare number of seconds.
func parseDuration(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(s)
}

func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return n
	}
	return def
}
