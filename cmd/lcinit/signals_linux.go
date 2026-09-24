//go:build linux

package main

import (
	"os"
	"syscall"
)

// forwardedSignals is deliberately a fixed list, not "everything" -
// SIGCHLD (a bookkeeping signal about lcinit's own child, not one to
// forward to it) and SIGURG (used internally by the Go runtime for
// goroutine preemption on some platforms) would be actively wrong to pass
// through.
var forwardedSignals = []os.Signal{
	syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT,
	syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGWINCH,
}
