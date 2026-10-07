package usage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func writeProc(t *testing.T, dir string, cpuLine string, memAvailKB int) {
	t.Helper()
	stat := cpuLine + "\ncpu0 1 1 1 1 0 0 0 0\ncpu1 1 1 1 1 0 0 0 0\nintr 5\n"
	mem := "MemTotal:        8000000 kB\nMemFree:          100 kB\nMemAvailable:   " + strconv.Itoa(memAvailKB) + " kB\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte(mem), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSampleReportsMachineAndTaskUsage(t *testing.T) {
	proc := t.TempDir()
	clock := time.Unix(1000, 0)
	cpuNanos := map[string]uint64{"c1": 0, "c2": 0}
	s := &Sampler{
		ProcRoot: proc, DataDir: "/data",
		Now:    func() time.Time { return clock },
		Statfs: func(string) (uint64, uint64, error) { return 100 << 30, 40 << 30, nil },
		Tasks: func() []Task {
			return []Task{
				{TaskID: "tsk_a", ContainerID: "c1"},
				{TaskID: "tsk_b", ContainerID: "c2", Tunnel: func() (int64, int64) { return 11, 22 }},
			}
		},
		Stats: func(_ context.Context, id string) (Container, error) {
			return Container{CPUNanos: cpuNanos[id], MemoryBytes: 64 << 20}, nil
		},
	}

	// First sample: no previous reading, so no rates yet, but memory and disk are known.
	writeProc(t, proc, "cpu  100 0 100 800 0 0 0 0", 6_000_000)
	first := s.Sample(context.Background())
	if first == nil || first.IntervalMs != 0 || first.HostCpuBusy != 0 {
		t.Fatalf("first sample should have no rates: %+v", first)
	}
	if first.HostCpuCount != 2 || first.HostMemTotalBytes != 8_000_000*1024 || first.HostMemUsedBytes != 2_000_000*1024 {
		t.Fatalf("machine totals wrong: %+v", first)
	}
	if first.DiskTotalBytes != 100<<30 || first.DiskUsedBytes != 60<<30 {
		t.Fatalf("disk wrong: %+v", first)
	}

	// 15 s later: the host spent 150 of 300 ticks busy; task a used 7.5 s of CPU (0.5 core),
	// task b 15 s (1 core).
	clock = clock.Add(15 * time.Second)
	cpuNanos["c1"] = 7_500_000_000
	cpuNanos["c2"] = 15_000_000_000
	writeProc(t, proc, "cpu  200 0 150 950 0 0 0 0", 6_000_000)
	second := s.Sample(context.Background())
	if second.IntervalMs != 15_000 {
		t.Fatalf("interval = %d", second.IntervalMs)
	}
	if second.HostCpuBusy < 0.49 || second.HostCpuBusy > 0.51 {
		t.Fatalf("host busy = %v, want 0.5 (150 busy of 300)", second.HostCpuBusy)
	}
	if len(second.Tasks) != 2 {
		t.Fatalf("tasks: %+v", second.Tasks)
	}
	a, b := second.Tasks[0], second.Tasks[1]
	if a.TaskId != "tsk_a" || a.CpuCores < 0.499 || a.CpuCores > 0.501 || a.MemoryBytes != 64<<20 {
		t.Fatalf("task a: %+v", a)
	}
	if b.TaskId != "tsk_b" || b.CpuCores < 0.999 || b.CpuCores > 1.001 || b.TunnelBytesToGateway != 11 || b.TunnelBytesToTask != 22 {
		t.Fatalf("task b: %+v", b)
	}
}

func TestSampleSkipsAContainerThatJustExitedAndCapsTheTaskList(t *testing.T) {
	proc := t.TempDir()
	writeProc(t, proc, "cpu  1 0 1 8 0 0 0 0", 1000)
	var tasks []Task
	for i := 0; i < MaxTasks+10; i++ {
		tasks = append(tasks, Task{TaskID: "tsk_" + strconv.Itoa(i), ContainerID: "c" + strconv.Itoa(i)})
	}
	s := &Sampler{
		ProcRoot: proc, Tasks: func() []Task { return tasks },
		Stats: func(_ context.Context, id string) (Container, error) {
			if id == "c0" {
				return Container{}, errors.New("no such container")
			}
			return Container{MemoryBytes: 1}, nil
		},
	}
	got := s.Sample(context.Background())
	if len(got.Tasks) != MaxTasks {
		t.Fatalf("want the list capped at %d, got %d", MaxTasks, len(got.Tasks))
	}
	for _, tu := range got.Tasks {
		if tu.TaskId == "tsk_0" {
			t.Fatal("a container that failed to read must be skipped, not reported as zero")
		}
	}
}

func TestSampleWithoutProcIsNil(t *testing.T) {
	s := &Sampler{ProcRoot: t.TempDir()}
	if s.Sample(context.Background()) != nil {
		t.Fatal("no /proc means nothing useful to say")
	}
}

func TestACounterThatGoesBackwardsIsNotANegativeRate(t *testing.T) {
	proc := t.TempDir()
	clock := time.Unix(0, 0)
	n := uint64(10_000_000_000)
	s := &Sampler{
		ProcRoot: proc, Now: func() time.Time { return clock },
		Tasks: func() []Task { return []Task{{TaskID: "t", ContainerID: "c"}} },
		Stats: func(context.Context, string) (Container, error) { return Container{CPUNanos: n}, nil },
	}
	writeProc(t, proc, "cpu  1 0 1 8 0 0 0 0", 1000)
	s.Sample(context.Background())
	clock = clock.Add(15 * time.Second)
	n = 1_000_000_000 // e.g. the container was recreated
	got := s.Sample(context.Background())
	if got.Tasks[0].CpuCores != 0 {
		t.Fatalf("cpu = %v, want 0", got.Tasks[0].CpuCores)
	}
}
