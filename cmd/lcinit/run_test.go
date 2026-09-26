//go:build integration

// lcinit only makes sense on Linux (it execs and signals a child as
// container PID 1); these tests spawn real /bin/sh children and are
// tagged integration like the rest of the suite's Linux-only tests.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestInitDeadline is task 3.6's own verify: a container whose agent and
// systemd are both gone (nothing left to enforce anything from outside)
// still exits at its wall timeout, because lcinit itself - running as the
// container's own PID 1 - enforces it.
func TestInitDeadline(t *testing.T) {
	start := time.Now()
	code, err := run([]string{"--max-duration=500ms", "--", "sleep", "30"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != deadlineExitCode {
		t.Fatalf("exit code = %d, want %d (deadline)", code, deadlineExitCode)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("deadline enforcement took %v, way past the 500ms max-duration + kill grace", elapsed)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("returned before max-duration(500ms) even elapsed: %v", elapsed)
	}
}

// TestChildExitCodePassesThrough proves the common case isn't broken by
// the deadline machinery: a child that exits on its own propagates its own
// exit code untouched.
func TestChildExitCodePassesThrough(t *testing.T) {
	code, err := run([]string{"--", "sh", "-c", "exit 7"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
}

// TestWaitFileBlocksUntilPresent is the regression test for a real race
// caught live: a task with tunnel_targets could run its very first
// command before internal/agent/netns's proxy had finished wiring up the
// container's one network exception, seeing no route/no DNS for a target
// that came up correctly a moment later. --wait-file exists to close that
// window; this proves the child genuinely doesn't start until the file
// appears, not just that it eventually starts regardless.
func TestWaitFileBlocksUntilPresent(t *testing.T) {
	dir := t.TempDir()
	readyFile := filepath.Join(dir, "ready")
	outFile := filepath.Join(dir, "out")

	doneCh := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := run([]string{"--wait-file=" + readyFile, "--", "sh", "-c", fmt.Sprintf("echo started > %s", outFile)})
		doneCh <- struct {
			code int
			err  error
		}{code, err}
	}()

	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(outFile); err == nil {
		t.Fatal("child started before the wait-file was ever created")
	}

	if err := os.WriteFile(readyFile, nil, 0o644); err != nil {
		t.Fatalf("creating ready file: %v", err)
	}

	select {
	case res := <-doneCh:
		if res.err != nil {
			t.Fatalf("run: %v", res.err)
		}
		if res.code != 0 {
			t.Fatalf("exit code = %d, want 0", res.code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() never returned after the wait-file appeared")
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("child never ran: %v", err)
	}
}

// TestWaitFileTimesOutAndStartsAnyway proves the wait is bounded and
// fails open: if the readiness signal never arrives (the proxy failed, or
// nothing is watching this flag at all), the child still starts rather
// than hanging forever - failing to wait is a reachability problem for
// the task, not a reason to never run it.
func TestWaitFileTimesOutAndStartsAnyway(t *testing.T) {
	oldTimeout, oldPoll := waitFileTimeout, waitFilePoll
	waitFileTimeout, waitFilePoll = 200*time.Millisecond, 20*time.Millisecond
	defer func() { waitFileTimeout, waitFilePoll = oldTimeout, oldPoll }()

	code, err := run([]string{"--wait-file=" + filepath.Join(t.TempDir(), "never-created"), "--", "sh", "-c", "exit 0"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (child must still run after the wait times out)", code)
	}
}

// TestSignalForwarding proves lcinit forwards a real signal to the child
// rather than only ever killing it via the deadline path: a child that
// traps SIGTERM and exits with a distinctive code should see it.
func TestSignalForwarding(t *testing.T) {
	script := `trap 'exit 42' TERM; while true; do sleep 0.05; done`
	doneCh := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := run([]string{"--", "sh", "-c", script})
		doneCh <- struct {
			code int
			err  error
		}{code, err}
	}()

	// Give the child a moment to install its trap, then signal the whole
	// test process group the same way an external SIGTERM to lcinit's own
	// PID would arrive - run()'s signal.Notify picks it up and forwards it.
	time.Sleep(200 * time.Millisecond)
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("finding self: %v", err)
	}
	if err := self.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling self: %v", err)
	}

	select {
	case res := <-doneCh:
		if res.err != nil {
			t.Fatalf("run: %v", res.err)
		}
		if res.code != 42 {
			t.Fatalf("exit code = %d, want 42 (child's SIGTERM trap)", res.code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() never returned after SIGTERM")
	}
}
