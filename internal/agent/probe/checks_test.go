package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCgroupVersionFS(t *testing.T) {
	dir := t.TempDir()
	fs := FS{CgroupControllersPath: filepath.Join(dir, "cgroup.controllers")}

	if got := CheckCgroupVersionFS(fs).Status; got != Fail {
		t.Fatalf("expected Fail when controllers file missing, got %s", got)
	}

	if err := os.WriteFile(fs.CgroupControllersPath, []byte("cpu memory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CheckCgroupVersionFS(fs).Status; got != Pass {
		t.Fatalf("expected Pass when controllers file present, got %s", got)
	}
}

func TestCheckSubuidFS(t *testing.T) {
	dir := t.TempDir()
	fs := FS{
		SubuidPath: filepath.Join(dir, "subuid"),
		SubgidPath: filepath.Join(dir, "subgid"),
	}

	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(fs.SubuidPath, "alice:100000:65536\n")
	write(fs.SubgidPath, "alice:100000:65536\n")
	if got := CheckSubuidFS(fs, "alice").Status; got != Pass {
		t.Fatalf("expected Pass with 65536 range, got %s", got)
	}

	write(fs.SubuidPath, "alice:100000:1000\n")
	if got := CheckSubuidFS(fs, "alice").Status; got != Fail {
		t.Fatalf("expected Fail with undersized range, got %s", got)
	}

	if got := CheckSubuidFS(fs, "bob").Status; got != Fail {
		t.Fatalf("expected Fail for user with no entries, got %s", got)
	}
}

func TestCheckMemoryLimitOOM(t *testing.T) {
	fr := newFakeRunner().
		on("run", CmdResult{ExitCode: 137}).
		on("inspect", CmdResult{Stdout: "true\n"})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}

	res := pm.CheckMemoryLimit(context.Background())
	if res.Status != Pass {
		t.Fatalf("expected Pass, got %s (%s)", res.Status, res.Detail)
	}
}

func TestCheckMemoryLimitNotEnforced(t *testing.T) {
	fr := newFakeRunner().
		on("run", CmdResult{ExitCode: 0}).
		on("inspect", CmdResult{Stdout: "false\n"})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}

	res := pm.CheckMemoryLimit(context.Background())
	if res.Status != Fail {
		t.Fatalf("expected Fail when not OOM-killed, got %s", res.Status)
	}
}

// TestCheckMemoryLimitENOSPC covers the other real enforcement path: on a
// real kernel, a tmpfs write that exceeds memory.max fails with ENOSPC
// rather than triggering the OOM killer - the write syscall just errors out
// and the process (here, dd) is free to exit non-zero on its own instead of
// being killed. This is standard, documented cgroup v2 behaviour, not a
// sign enforcement is missing, and was found live on a real Ubuntu VM
// (WSL2's kernel never enforces this at all, so it never surfaced there).
func TestCheckMemoryLimitENOSPC(t *testing.T) {
	fr := newFakeRunner().
		on("run", CmdResult{ExitCode: 1}).
		on("inspect", CmdResult{Stdout: "false\n"}).
		on("logs", CmdResult{Stderr: "62+1 records in\n62+0 records out\n65536000 bytes copied\n"})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}

	res := pm.CheckMemoryLimit(context.Background())
	if res.Status != Pass {
		t.Fatalf("expected Pass on a short ENOSPC write, got %s (%s)", res.Status, res.Detail)
	}
}

// TestCheckMemoryLimitFullWriteExitedNonZero guards against the ENOSPC path
// masking an unrelated failure: a full 128MB write that happens to exit
// non-zero for some other reason must still fail the check, since the
// memory limit plainly did not stop anything.
func TestCheckMemoryLimitFullWriteExitedNonZero(t *testing.T) {
	fr := newFakeRunner().
		on("run", CmdResult{ExitCode: 1}).
		on("inspect", CmdResult{Stdout: "false\n"}).
		on("logs", CmdResult{Stderr: "128+0 records in\n128+0 records out\n134217728 bytes copied\nsh: some unrelated error\n"})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}

	res := pm.CheckMemoryLimit(context.Background())
	if res.Status != Fail {
		t.Fatalf("expected Fail when the full write completed despite a nonzero exit, got %s", res.Status)
	}
}

func TestCheckCPUQuotaThrottled(t *testing.T) {
	fr := newFakeRunner().
		on("run", CmdResult{Stdout: "500000\n"}). // capped: half the work
		on("run", CmdResult{Stdout: "1000000\n"}) // uncapped
	pm := Podman{Runner: fr, ProbeImage: "alpine"}

	res := pm.CheckCPUQuota(context.Background())
	if res.Status != Pass {
		t.Fatalf("expected Pass, got %s (%s)", res.Status, res.Detail)
	}
}

func TestCheckCPUQuotaNotThrottled(t *testing.T) {
	fr := newFakeRunner().
		on("run", CmdResult{Stdout: "990000\n"}). // capped run barely slower
		on("run", CmdResult{Stdout: "1000000\n"})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}

	res := pm.CheckCPUQuota(context.Background())
	if res.Status != Fail {
		t.Fatalf("expected Fail when not measurably throttled, got %s", res.Status)
	}
}

func TestCheckPIDsLimit(t *testing.T) {
	fr := newFakeRunner().on("run", CmdResult{ExitCode: 1})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}
	if got := pm.CheckPIDsLimit(context.Background()).Status; got != Pass {
		t.Fatalf("expected Pass when forking beyond limit fails, got %s", got)
	}

	fr2 := newFakeRunner().on("run", CmdResult{ExitCode: 0})
	pm2 := Podman{Runner: fr2, ProbeImage: "alpine"}
	if got := pm2.CheckPIDsLimit(context.Background()).Status; got != Fail {
		t.Fatalf("expected Fail when 64 forks succeed under a 16 limit, got %s", got)
	}
}

func TestCheckGVisorSkipsWhenAbsent(t *testing.T) {
	pm := Podman{Runner: newFakeRunner(), ProbeImage: "alpine"}
	lookPath := func(string) (string, error) { return "", errors.New("not found") }
	if got := pm.CheckGVisor(context.Background(), lookPath).Status; got != Skipped {
		t.Fatalf("expected Skipped, got %s", got)
	}
}

func TestCheckGVisorPassesWhenRunnable(t *testing.T) {
	fr := newFakeRunner().on("run", CmdResult{ExitCode: 0})
	pm := Podman{Runner: fr, ProbeImage: "alpine"}
	lookPath := func(string) (string, error) { return "/usr/bin/runsc", nil }
	if got := pm.CheckGVisor(context.Background(), lookPath).Status; got != Pass {
		t.Fatalf("expected Pass, got %s", got)
	}
}

func TestCheckSystemdDelegate(t *testing.T) {
	fr := newFakeRunner().on("systemctl", CmdResult{Stdout: "Delegate=yes\n"})
	if got := CheckSystemdDelegate(context.Background(), fr).Status; got != Pass {
		t.Fatalf("expected Pass, got %s", got)
	}

	fr2 := newFakeRunner().on("systemctl", CmdResult{Stdout: "Delegate=no\n"})
	if got := CheckSystemdDelegate(context.Background(), fr2).Status; got != Fail {
		t.Fatalf("expected Fail, got %s", got)
	}
}

func TestReportOK(t *testing.T) {
	r := Report{Results: []Result{
		{Name: CheckMemoryLimit, Status: Pass},
		{Name: CheckCPUQuota, Status: Pass},
		{Name: CheckDiskLimit, Status: Fail},
	}}
	if !r.OK() {
		t.Fatal("expected OK when memory and cpu pass, even if disk fails")
	}

	r2 := Report{Results: []Result{
		{Name: CheckMemoryLimit, Status: Fail},
		{Name: CheckCPUQuota, Status: Pass},
	}}
	if r2.OK() {
		t.Fatal("expected not-OK when memory fails")
	}
}
