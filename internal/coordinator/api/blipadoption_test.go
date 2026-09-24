package api

import (
	"context"
	"testing"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// TestBlipAdoption is task 3.3's own verify: on reconnect, the agent
// re-announces its running task IDs in Register, and the coordinator either
// adopts a task it still considers assigned to that node (no message sent -
// the assignment already exists) or, for a task it has already reclaimed,
// sends a Cancel telling the agent to kill its local copy so it can never
// double-report or block a redispatch elsewhere.
func TestBlipAdoption(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("good-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenAgent})
	client, _ := startTestServer(t, fs)

	// First connect: register the node once, matching what a real agent
	// process would do on startup - instance_id is what lets a later
	// reconnect be recognised as the same node.
	nodeID := register(t, client, "ins_blip", nil)

	now := time.Now()
	nodeIDPtr := nodeID
	future := now.Add(2 * time.Minute)
	past := now.Add(-1 * time.Minute)

	// adopted: still assigned to this node, requeue hasn't fired.
	fs.tasks["tsk_adopt"] = store.Task{
		ID: "tsk_adopt", AccountID: "act_1", State: store.TaskRunning,
		NodeID: &nodeIDPtr, RequeueAfter: &future,
	}
	// reclaimed: requeue_after already passed - the coordinator has moved
	// on even though nothing has rewritten the row's state yet (task 3.4's
	// reclaimer loop isn't built; the deadline itself is what's
	// authoritative per task 3.1's invariant).
	fs.tasks["tsk_reclaimed_by_time"] = store.Task{
		ID: "tsk_reclaimed_by_time", AccountID: "act_1", State: store.TaskDispatched,
		NodeID: &nodeIDPtr, RequeueAfter: &past,
	}
	// reclaimed: reassigned to a different node entirely.
	otherNode := "nod_other"
	fs.tasks["tsk_reassigned"] = store.Task{
		ID: "tsk_reassigned", AccountID: "act_1", State: store.TaskDispatched,
		NodeID: &otherNode, RequeueAfter: &future,
	}
	// reclaimed: already finished (agent is behind on its own bookkeeping).
	fs.tasks["tsk_finished"] = store.Task{
		ID: "tsk_finished", AccountID: "act_1", State: store.TaskSucceeded,
		NodeID: &nodeIDPtr, RequeueAfter: &future,
	}

	// Reconnect ("the blip"): a fresh Connect stream, same instance_id, and
	// the agent re-announces all four as still running locally.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{
		Register: &lazycakev1.Register{
			Token: "good-token", Hostname: "host1", Arch: "amd64", InstanceId: "ins_blip",
			RunningTaskIds: []string{"tsk_adopt", "tsk_reclaimed_by_time", "tsk_reassigned", "tsk_finished"},
		},
	}}); err != nil {
		t.Fatalf("send register: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv register ack: %v", err)
	}
	ack := msg.GetRegisterAck()
	if ack == nil {
		t.Fatalf("expected RegisterAck, got %+v", msg)
	}
	if ack.GetNodeId() != nodeID {
		t.Fatalf("reconnect got a different node_id: %s != %s (adoption requires a stable node identity)", ack.GetNodeId(), nodeID)
	}

	// Collect whatever Cancels arrive within a short window; nothing more
	// should ever come after that on an otherwise idle stream.
	cancelled := map[string]bool{}
	deadline := time.After(2 * time.Second)
collect:
	for {
		msgCh := make(chan *lazycakev1.CoordinatorMessage, 1)
		errCh := make(chan error, 1)
		go func() {
			m, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			msgCh <- m
		}()
		select {
		case m := <-msgCh:
			if c := m.GetCancel(); c != nil {
				cancelled[c.GetTaskId()] = true
			}
		case <-errCh:
			break collect
		case <-deadline:
			break collect
		}
	}

	want := map[string]bool{
		"tsk_reclaimed_by_time": true,
		"tsk_reassigned":        true,
		"tsk_finished":          true,
	}
	for taskID := range want {
		if !cancelled[taskID] {
			t.Errorf("expected a Cancel for reclaimed task %s, got none", taskID)
		}
	}
	if cancelled["tsk_adopt"] {
		t.Errorf("tsk_adopt should have been silently adopted, not cancelled")
	}
}

// register performs one Connect+Register round trip and returns the node ID
// the coordinator assigned.
func register(t *testing.T, client lazycakev1.AgentServiceClient, instanceID string, running []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{
		Register: &lazycakev1.Register{
			Token: "good-token", Hostname: "host1", Arch: "amd64",
			InstanceId: instanceID, RunningTaskIds: running,
		},
	}}); err != nil {
		t.Fatalf("send register: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv register ack: %v", err)
	}
	ack := msg.GetRegisterAck()
	if ack == nil || ack.GetNodeId() == "" {
		t.Fatalf("expected RegisterAck with node id, got %+v", msg)
	}
	cancel()
	return ack.GetNodeId()
}
