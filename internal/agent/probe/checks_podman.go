package probe

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Podman groups the container-based checks: each actually starts a scratch
// container and inspects what happened, rather than trusting a version
// string. ProbeImage must be a small, already-pullable image (alpine is
// used in production).
type Podman struct {
	Runner     Runner
	Bin        string // "podman" by default
	ProbeImage string // e.g. "alpine:latest"
}

func (p Podman) bin() string {
	if p.Bin == "" {
		return "podman"
	}
	return p.Bin
}

func (p Podman) run(ctx context.Context, args ...string) (CmdResult, error) {
	return p.Runner.Run(ctx, p.bin(), args...)
}

// CheckMemoryLimit starts a container capped at 64MB that writes 128MB into
// tmpfs, and confirms the kernel OOM-kills it - i.e. the cgroup memory
// limit is actually enforced, not just accepted as a flag.
func (p Podman) CheckMemoryLimit(ctx context.Context) Result {
	name := "lazycake-probe-mem-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	defer p.run(context.Background(), "rm", "-f", name)

	_, err := p.run(ctx, "run", "--name", name, "--memory=64m",
		p.ProbeImage, "sh", "-c", "dd if=/dev/zero of=/dev/shm/fill bs=1M count=128")
	if err != nil {
		return fail(CheckMemoryLimit, "running probe container: %v", err)
	}

	inspect, err := p.run(ctx, "inspect", "--format", "{{.State.OOMKilled}}", name)
	if err != nil {
		return fail(CheckMemoryLimit, "inspecting probe container: %v", err)
	}
	if strings.TrimSpace(inspect.Stdout) != "true" {
		return fail(CheckMemoryLimit, "--memory=64m did not OOM-kill a 128MB write; cgroups v2 memory delegation is likely missing (see PLAN.md 'Rootless constraints')")
	}
	return pass(CheckMemoryLimit)
}

// CheckCPUQuota starts two identical CPU-bound busy loops, one capped at
// --cpus=0.5 and one uncapped, both timed to the same wall-clock budget,
// and confirms the capped one does roughly half the work.
func (p Podman) CheckCPUQuota(ctx context.Context) Result {
	const script = `i=0; s=$(date +%s); while [ $(( $(date +%s) - s )) -lt 2 ]; do i=$((i+1)); done; echo $i`

	capped, err := p.run(ctx, "run", "--rm", "--cpus=0.5", p.ProbeImage, "sh", "-c", script)
	if err != nil {
		return fail(CheckCPUQuota, "running capped probe: %v", err)
	}
	uncapped, err := p.run(ctx, "run", "--rm", p.ProbeImage, "sh", "-c", script)
	if err != nil {
		return fail(CheckCPUQuota, "running uncapped probe: %v", err)
	}

	cappedN, err1 := strconv.ParseFloat(strings.TrimSpace(capped.Stdout), 64)
	uncappedN, err2 := strconv.ParseFloat(strings.TrimSpace(uncapped.Stdout), 64)
	if err1 != nil || err2 != nil || uncappedN == 0 {
		return fail(CheckCPUQuota, "unexpected probe output: capped=%q uncapped=%q", capped.Stdout, uncapped.Stdout)
	}

	ratio := cappedN / uncappedN
	// A real 0.5 quota should land near 0.5x the iterations; allow a wide
	// band since this is a noisy VM-hosted measurement, not a hard bound.
	if ratio > 0.85 {
		return fail(CheckCPUQuota, "--cpus=0.5 did not measurably throttle (capped/uncapped iteration ratio %.2f, expected <= 0.85); cpu quota enforcement is likely missing", ratio)
	}
	return pass(CheckCPUQuota)
}

// CheckPIDsLimit starts a container capped at 16 PIDs and confirms it
// cannot fork past that limit.
func (p Podman) CheckPIDsLimit(ctx context.Context) Result {
	res, err := p.run(ctx, "run", "--rm", "--pids-limit=16", p.ProbeImage,
		"sh", "-c", "for i in $(seq 1 64); do sleep 30 & done; wait")
	if err != nil {
		return fail(CheckPIDsLimit, "running probe container: %v", err)
	}
	if res.ExitCode == 0 {
		return fail(CheckPIDsLimit, "--pids-limit=16 did not stop 64 forks (exit 0); pids controller is likely missing")
	}
	return pass(CheckPIDsLimit)
}

// CheckGVisor reports whether the runsc OCI runtime is usable: present on
// PATH and able to actually run a scratch container.
func (p Podman) CheckGVisor(ctx context.Context, lookPath func(string) (string, error)) Result {
	if _, err := lookPath("runsc"); err != nil {
		return skip(CheckGVisor, "runsc not found on PATH")
	}
	res, err := p.run(ctx, "run", "--rm", "--runtime=runsc", p.ProbeImage, "true")
	if err != nil || res.ExitCode != 0 {
		return fail(CheckGVisor, "runsc is on PATH but a scratch container under it failed: %v (exit %d): %s", err, res.ExitCode, res.Stderr)
	}
	return pass(CheckGVisor)
}
