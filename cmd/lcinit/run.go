package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// deadlineExitCode mirrors the `timeout(1)` convention, so anything
// inspecting a container's exit code can tell "hit max_duration" apart
// from an ordinary failure.
const deadlineExitCode = 124

// killGrace is how long a child gets to exit after SIGTERM (deadline or
// forwarded) before lcinit escalates to SIGKILL.
const killGrace = 5 * time.Second

// run execs args[1:] (after any lcinit flags and a "--" separator) as a
// child, forwards signals to it, and enforces --max-duration as a hard
// deadline regardless of whether anything else (the agent, systemd) is
// still around to enforce it - PLAN.md "Killing orphans": this is the
// backstop for when both of those are gone. Returns the exit code the
// process should exit with; err is non-nil only for a genuine lcinit-level
// failure (bad flags, couldn't start the child), not a nonzero child exit.
func run(args []string) (int, error) {
	fs := flag.NewFlagSet("lcinit", flag.ContinueOnError)
	maxDuration := fs.Duration("max-duration", 0, "hard wall-clock deadline for the child; 0 disables it")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return 1, errors.New("no command given (usage: lcinit [--max-duration=D] -- <cmd> [args...])")
	}

	ctx := context.Background()
	if *maxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *maxDuration)
		defer cancel()
	}

	cmd := exec.Command(rest[0], rest[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting %s: %w", rest[0], err)
	}

	sigCh := make(chan os.Signal, 16)
	signal.Notify(sigCh, forwardedSignals...)
	defer signal.Stop(sigCh)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	for {
		select {
		case sig := <-sigCh:
			_ = cmd.Process.Signal(sig)
		case <-ctx.Done():
			terminate(cmd, done)
			return deadlineExitCode, nil
		case err := <-done:
			if err == nil {
				return 0, nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return exitErr.ExitCode(), nil
			}
			return 1, fmt.Errorf("waiting for %s: %w", rest[0], err)
		}
	}
}

// terminate gives the child killGrace to exit on its own after SIGTERM,
// then SIGKILLs it - lcinit itself must not outlive max_duration by more
// than that either.
func terminate(cmd *exec.Cmd, done <-chan error) {
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(killGrace):
		_ = cmd.Process.Kill()
		<-done
	}
}
