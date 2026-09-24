package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// CheckDiskLimit attempts the sparse-file-per-task mechanism PLAN.md
// settles on for rootless disk quotas: create a sparse ext4 image, mkfs it,
// and mount it via a user namespace (no setuid helper, no root). This is
// the hard one - see PLAN.md "Per-task disk limits are the hard one" - so
// failure here is expected on many hosts and only removes the capability,
// it doesn't block the agent.
func (p Podman) CheckDiskLimit(ctx context.Context, workDir string) Result {
	imgPath := filepath.Join(workDir, "lazycake-probe-disk.img")
	mountPath := filepath.Join(workDir, "lazycake-probe-disk-mnt")
	defer os.Remove(imgPath)
	defer os.RemoveAll(mountPath)

	if err := os.MkdirAll(mountPath, 0o700); err != nil {
		return fail(CheckDiskLimit, "creating mount point: %v", err)
	}

	f, err := os.Create(imgPath)
	if err != nil {
		return fail(CheckDiskLimit, "creating sparse file: %v", err)
	}
	if err := f.Truncate(64 << 20); err != nil { // 64MB sparse
		f.Close()
		return fail(CheckDiskLimit, "truncating sparse file: %v", err)
	}
	f.Close()

	mkfs, err := p.Runner.Run(ctx, "mkfs.ext4", "-q", "-F", imgPath)
	if err != nil || mkfs.ExitCode != 0 {
		return fail(CheckDiskLimit, "mkfs.ext4 unavailable or failed (%v): %s", err, mkfs.Stderr)
	}

	// Rootless mount: a private mount namespace plus a root-mapped user
	// namespace lets an unprivileged user mount a loop-backed image it
	// owns, without any setuid helper.
	mount, err := p.Runner.Run(ctx, "unshare", "--mount", "--map-root-user", "--",
		"mount", "-o", "loop", imgPath, mountPath)
	if err != nil || mount.ExitCode != 0 {
		return fail(CheckDiskLimit,
			"rootless loop mount failed (%v): %s; disk limits will fall back to writable-layer monitoring only (see PLAN.md)",
			err, strings.TrimSpace(mount.Stderr))
	}
	// The mount above lived only inside unshare's own mount namespace, so
	// there is nothing left here to unmount on this side.
	return pass(CheckDiskLimit)
}
