package probe

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// FS is the filesystem seam for checks that just read files, so tests can
// point at a temp directory instead of the real /sys and /etc.
type FS struct {
	CgroupControllersPath string // default /sys/fs/cgroup/cgroup.controllers
	SubuidPath            string // default /etc/subuid
	SubgidPath            string // default /etc/subgid
}

// DefaultFS points at the real paths on a Linux host.
func DefaultFS() FS {
	return FS{
		CgroupControllersPath: "/sys/fs/cgroup/cgroup.controllers",
		SubuidPath:            "/etc/subuid",
		SubgidPath:            "/etc/subgid",
	}
}

// CheckCgroupVersionFS reports v2 if cgroup.controllers exists (v1 has no
// such file - it uses a different hierarchy entirely).
func CheckCgroupVersionFS(fs FS) Result {
	if _, err := os.Stat(fs.CgroupControllersPath); err != nil {
		return fail(CheckCgroupVersion, "cgroups v1 detected (no %s); lazycake requires v2 with the unified hierarchy", fs.CgroupControllersPath)
	}
	return pass(CheckCgroupVersion)
}

// CheckSubuidFS requires at least 65536 sub-UIDs and sub-GIDs for
// currentUser, the minimum a single rootless container needs.
func CheckSubuidFS(fs FS, currentUser string) Result {
	uidCount, err := subRangeSize(fs.SubuidPath, currentUser)
	if err != nil {
		return fail(CheckSubuid, "reading %s: %v", fs.SubuidPath, err)
	}
	gidCount, err := subRangeSize(fs.SubgidPath, currentUser)
	if err != nil {
		return fail(CheckSubuid, "reading %s: %v", fs.SubgidPath, err)
	}
	const min = 65536
	if uidCount < min || gidCount < min {
		return fail(CheckSubuid,
			"user %q has only %d subuid / %d subgid entries, need >= %d each; run: usermod --add-subuids %d-%d --add-subgids %d-%d %s",
			currentUser, uidCount, gidCount, min, min, min*2-1, min, min*2-1, currentUser)
	}
	return pass(CheckSubuid)
}

// subRangeSize sums the range sizes for user across every matching line, so
// a user with multiple non-contiguous ranges is measured correctly.
func subRangeSize(path, user string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	total := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(strings.TrimSpace(sc.Text()), ":")
		if len(parts) != 3 || parts[0] != user {
			continue
		}
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		total += n
	}
	return total, sc.Err()
}
