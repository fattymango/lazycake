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
// container disappears. rc must already be an open log-follow stream
// (see run()'s call site: Runtime.Logs is called before Start, not here,
// so the subscription exists before the container can possibly produce
// or finish producing output - opening it only once this goroutine
// happens to get scheduled raced Start for a fast-exiting container and
// could lose the whole thing).
// linesRelayed returns how many lines were actually sent, so the caller
// can tell a genuinely silent container apart from one whose follow-mode
// attach raced the container's own exit and missed everything (see
// run()'s fallbackLogs call).
func (e *Executor) streamLogs(ctx context.Context, taskID string, rc io.ReadCloser) (linesRelayed int64) {
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
				return r.seq
			}
			r.add(line)
		case <-ticker.C:
			r.flush()
		case <-ctx.Done():
			r.flush()
			return r.seq
		}
	}
}

// fallbackLogs does a one-shot, non-follow read of a container's complete
// persisted log (Runtime.Logs(..., follow=false), reliable against an
// already-exited container in a way a follow-mode attach isn't - see
// runtime.Runtime.Logs's doc comment) and relays it as if it had streamed
// live. Only called when streamLogs relayed nothing at all, so this task
// genuinely has no output already sent to lose or duplicate.
func (e *Executor) fallbackLogs(ctx context.Context, taskID, containerID string) {
	rc, err := e.Runtime.Logs(ctx, containerID, false)
	if err != nil {
		e.Log.Warn("fallback log read", "task_id", taskID, "container_id", containerID, "error", err)
		return
	}
	defer rc.Close()

	r := &logRelay{taskID: taskID, send: e.Send.Send, windowStart: time.Now()}
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		r.add(sc.Text())
	}
	r.flush()
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
