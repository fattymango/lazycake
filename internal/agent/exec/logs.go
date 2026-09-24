package exec

import (
	"bufio"
	"context"
	"io"
	"time"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// Log relay limits, per IMPLEMENTATION.md task 1.9. A container emitting
// unbounded output must never fill the host's disk or OOM the agent, and
// must never block the coordinator's slowness onto the task itself.
const (
	logFlushInterval = 250 * time.Millisecond
	logFlushBytes    = 64 * 1024
	logRateLimit     = 1 << 20  // 1MB/s
	logTotalCap      = 50 << 20 // 50MB/task
	truncationLine   = "[lazycake] log output truncated at 50MB"
)

// streamLogs reads a container's combined stdout/stderr and relays it as
// batched LogBatch messages, rate-limited and capped per task. Uses ctx
// (the connection's outer lifetime), not runCtx, so a wall-timeout-
// triggered Stop still lets the last lines drain through before the
// container disappears.
func (e *Executor) streamLogs(ctx context.Context, taskID, containerID string) {
	rc, err := e.Runtime.Logs(ctx, containerID)
	if err != nil {
		e.Log.Warn("streaming logs", "task_id", taskID, "container_id", containerID, "error", err)
		return
	}
	defer rc.Close()

	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				// Keep draining the reader so the container's stdout pipe
				// never backs up and blocks the task itself, even once
				// nothing downstream wants the lines any more.
				io.Copy(io.Discard, rc) //nolint:errcheck // best-effort drain
				return
			}
		}
	}()

	r := &logRelay{taskID: taskID, send: e.Send.Send, windowStart: time.Now()}
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				r.flush()
				return
			}
			r.add(line)
		case <-ticker.C:
			r.flush()
		case <-ctx.Done():
			r.flush()
			return
		}
	}
}

// logRelay batches, rate-limits and caps one task's log lines before
// handing them to Sender.Send. Not safe for concurrent use - streamLogs
// drives it from a single goroutine.
type logRelay struct {
	taskID string
	send   func(*lazycakev1.AgentMessage)

	seq        int64
	buf        []*lazycakev1.LogLine
	bufBytes   int
	totalBytes int64
	truncated  bool

	windowStart time.Time
	windowBytes int64
}

func (r *logRelay) add(line string) {
	if r.truncated {
		return // total cap already hit: keep draining upstream, send nothing more
	}
	if r.totalBytes+int64(len(line)) > logTotalCap {
		r.truncated = true
		r.appendLine(truncationLine)
		r.flush()
		return
	}

	now := time.Now()
	if now.Sub(r.windowStart) >= time.Second {
		r.windowStart = now
		r.windowBytes = 0
	}
	if r.windowBytes+int64(len(line)) > logRateLimit {
		return // over the 1MB/s budget for this window: drop, don't buffer
	}
	r.windowBytes += int64(len(line))

	r.appendLine(line)
	if r.bufBytes >= logFlushBytes {
		r.flush()
	}
}

func (r *logRelay) appendLine(line string) {
	r.seq++
	r.totalBytes += int64(len(line))
	r.bufBytes += len(line)
	r.buf = append(r.buf, &lazycakev1.LogLine{
		Seq: r.seq, Stream: "stdout", AtUnixMs: time.Now().UnixMilli(), Line: line,
	})
}

func (r *logRelay) flush() {
	if len(r.buf) == 0 {
		return
	}
	r.send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Logs{
		Logs: &lazycakev1.LogBatch{TaskId: r.taskID, Lines: r.buf},
	}})
	r.buf = nil
	r.bufBytes = 0
}
