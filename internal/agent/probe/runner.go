package probe

import (
	"bytes"
	"context"
	"os/exec"
)

// CmdResult is the outcome of running one command.
type CmdResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner runs a command and reports its result. It is the seam every check
// that shells out to podman depends on, so tests can fake podman's
// behaviour instead of needing a real container runtime.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (CmdResult, error)
}

// ExecRunner is the production Runner, backed by os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (CmdResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := CmdResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, err
	}
	return res, nil
}
