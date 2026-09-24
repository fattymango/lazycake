package probe

import (
	"context"
	"os"
	"os/exec"
	"os/user"
)

// Options configures a full Run: everything a caller might want to fake or
// point elsewhere for testing.
type Options struct {
	Runner     Runner
	PodmanBin  string
	ProbeImage string
	WorkDir    string
	FS         FS
	LookPath   func(string) (string, error)
	Username   func() (string, error)
}

// DefaultOptions wires the real environment: os/exec, /sys and /etc, and
// the current OS user.
func DefaultOptions() Options {
	return Options{
		Runner:     ExecRunner{},
		PodmanBin:  "podman",
		ProbeImage: "docker.io/library/alpine:latest",
		WorkDir:    os.TempDir(),
		FS:         DefaultFS(),
		LookPath:   exec.LookPath,
		Username: func() (string, error) {
			u, err := user.Current()
			if err != nil {
				return "", err
			}
			return u.Username, nil
		},
	}
}

// Run executes every check and returns the full report. Individual checks
// that error just report Fail with the error as their detail; Run itself
// only errors if something outside any single check (like resolving the
// current user) fails.
func Run(ctx context.Context, opt Options) (Report, error) {
	pm := Podman{Runner: opt.Runner, Bin: opt.PodmanBin, ProbeImage: opt.ProbeImage}

	username, err := opt.Username()
	if err != nil {
		return Report{}, err
	}

	results := []Result{
		CheckCgroupVersionFS(opt.FS),
		CheckSubuidFS(opt.FS, username),
		pm.CheckMemoryLimit(ctx),
		pm.CheckCPUQuota(ctx),
		pm.CheckPIDsLimit(ctx),
		pm.CheckDiskLimit(ctx, opt.WorkDir),
		CheckSystemdDelegate(ctx, opt.Runner),
		pm.CheckGVisor(ctx, opt.LookPath),
	}
	return Report{Results: results}, nil
}
