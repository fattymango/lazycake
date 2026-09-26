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

// waitFileTimeout/waitFilePoll bound --wait-file: internal/agent/netns's
// tunnel proxy touches this file once it's actually serving (see
// proxy.go's Setup), so a task with tunnel_targets doesn't start running
// before its one network exception exists yet - caught live: a task's
// very first command could race Setup and see no route/no DNS at all for
// a registered target, even though the tunnel came up correctly a moment
// later. Bounded rather than unconditional: if Setup never signals
// (proxy failed, or this flag is stale for some other reason), the child
// still starts - failing to wait is a reachability problem for the task,
// not a security one, since --network=none holds regardless.
var (
	waitFileTimeout = 10 * time.Second
	waitFilePoll    = 50 * time.Millisecond
)

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
	waitFile := fs.String("wait-file", "", "if set, poll for this file's existence (up to waitFileTimeout) before starting the child")
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

	if *waitFile != "" {
		waitForFile(*waitFile, waitFileTimeout, waitFilePoll)
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

// waitForFile polls for path's existence, returning as soon as it appears
// or once timeout elapses, whichever comes first - never returns an error,
// since a timed-out wait just means the child starts without its
// readiness signal, not that lcinit itself failed.
func waitForFile(path string, timeout, poll time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(poll)
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
