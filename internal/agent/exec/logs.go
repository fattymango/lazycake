package exec

import (
	"bufio"
	"context"
	"time"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// streamLogs reads a container's combined stdout/stderr and relays it as
// LogBatch messages. Uses ctx (the connection's outer lifetime), not
// runCtx, so a wall-timeout-triggered Stop still lets the last lines drain
// through before the container disappears.
//
// Batching cadence and the per-task byte cap land in task 1.9; this is the
// minimal version task 1.8 needs to make logs retrievable at all.
func (e *Executor) streamLogs(ctx context.Context, taskID, containerID string) {
	rc, err := e.Runtime.Logs(ctx, containerID)
	if err != nil {
		e.Log.Warn("streaming logs", "task_id", taskID, "container_id", containerID, "error", err)
		return
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var seq int64
	for sc.Scan() {
		seq++
		e.Send.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Logs{
			Logs: &lazycakev1.LogBatch{
				TaskId: taskID,
				Lines: []*lazycakev1.LogLine{{
					Seq:      seq,
					Stream:   "stdout",
					AtUnixMs: time.Now().UnixMilli(),
					Line:     sc.Text(),
				}},
			},
		}})
	}
}
