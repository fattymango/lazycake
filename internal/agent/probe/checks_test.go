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
