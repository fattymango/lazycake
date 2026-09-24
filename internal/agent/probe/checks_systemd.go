package probe

import (
	"context"
	"strings"
)

// CheckSystemdSlice confirms the user's systemd slice has Delegate=yes,
// which is what lets rootless Podman actually enforce cgroup limits (see
// PLAN.md "cgroups v2 delegation is load-bearing").
func CheckSystemdDelegate(ctx context.Context, r Runner) Result {
	res, err := r.Run(ctx, "systemctl", "--user", "show", "user.slice", "-p", "Delegate")
	if err != nil {
		return fail(CheckSystemdSlice, "running systemctl --user: %v", err)
	}
	if res.ExitCode != 0 {
		return fail(CheckSystemdSlice, "systemctl --user unavailable (exit %d): %s; enable lingering with 'loginctl enable-linger <user>' and log in via a user session", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	if strings.TrimSpace(res.Stdout) != "Delegate=yes" {
		return fail(CheckSystemdSlice, "user.slice does not have Delegate=yes (got %q); see PLAN.md 'cgroups v2 delegation is load-bearing'", strings.TrimSpace(res.Stdout))
	}
	return pass(CheckSystemdSlice)
}
