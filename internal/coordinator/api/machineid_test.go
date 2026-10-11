package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/auth"
	"github.com/fattymango/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

type restartRecorder struct {
	noopEvents
	mu    sync.Mutex
	calls []restartCall
}
type restartCall struct {
	node    string
	running []string
}

func (r *restartRecorder) OnAgentRestarted(_ context.Context, nodeID string, running []string) {
	r.mu.Lock()
	r.calls = append(r.calls, restartCall{nodeID, running})
	r.mu.Unlock()
}
func (r *restartRecorder) got() []restartCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]restartCall(nil), r.calls...)
}

func registerAs(t *testing.T, client lazycakev1.AgentServiceClient, hostname, machineID, instanceID string, running []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{Register: &lazycakev1.Register{
		Token: "good-token", Hostname: hostname, Arch: "amd64", MachineId: machineID, InstanceId: instanceID, RunningTaskIds: running,
	}}}); err != nil {
		t.Fatal(err)
	}
	msg, err := stream.Recv()
	if err != nil || msg.GetRegisterAck() == nil {
		t.Fatalf("expected RegisterAck, got %+v, %v", msg, err)
	}
	// Give the server a moment to run its post-ack registration steps before the stream is torn down.
	time.Sleep(50 * time.Millisecond)
	return msg.GetRegisterAck().GetNodeId()
}

func machineTestServer(t *testing.T) (lazycakev1.AgentServiceClient, *fakeStore, *restartRecorder) {
	t.Helper()
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("good-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenAgent})
	rec := &restartRecorder{}
	client, _ := startTestServerWithEvents(t, fs, rec)
	return client, fs, rec
}

// Task 8.19: restarting or updating an agent must not create a second machine entry.
func TestRestartOfTheSameMachineKeepsTheSameNode(t *testing.T) {
	client, fs, rec := machineTestServer(t)

	first := registerAs(t, client, "box", "mch_A", "ins_1", nil)
	second := registerAs(t, client, "box", "mch_A", "ins_2", []string{"tsk_still_here"}) // a new process: an update or a reboot
	if first != second {
		t.Fatalf("a restart made a new machine entry: %s then %s", first, second)
	}
	if len(nodesNow(fs)) != 1 {
		t.Fatalf("%d machine entries, want 1", len(nodesNow(fs)))
	}
	calls := rec.got()
	if len(calls) != 1 || calls[0].node != first || len(calls[0].running) != 1 || calls[0].running[0] != "tsk_still_here" {
		t.Fatalf("the scheduler must be told the agent restarted, with what it still runs: %+v", calls)
	}

	// The same process reconnecting after a network blip is not a restart: nothing is reclaimed.
	registerAs(t, client, "box", "mch_A", "ins_2", nil)
	if n := len(rec.got()); n != 1 {
		t.Fatalf("a reconnect of the same process was treated as a restart (%d calls)", n)
	}
}

func TestDifferentMachinesGetDifferentNodesEvenWithTheSameHostname(t *testing.T) {
	client, fs, _ := machineTestServer(t)
	a := registerAs(t, client, "ubuntu", "mch_A", "ins_1", nil)
	b := registerAs(t, client, "ubuntu", "mch_B", "ins_2", nil)
	if a == b || len(nodesNow(fs)) != 2 {
		t.Fatalf("two machines were merged: %s, %s (%d entries)", a, b, len(nodesNow(fs)))
	}
}

// An agent that predates machine ids keeps the old behaviour: each process is its own entry.
func TestAgentWithoutMachineIDIsStillPerProcess(t *testing.T) {
	client, _, rec := machineTestServer(t)
	a := registerAs(t, client, "box", "", "ins_1", nil)
	b := registerAs(t, client, "box", "", "ins_2", nil)
	if a == b {
		t.Fatal("agents without a machine id must stay per-process")
	}
	if len(rec.got()) != 0 {
		t.Fatal("no restart handling without a machine id")
	}
}

// The first time a machine reports an id, the entry it already had is taken over instead of a duplicate being added, but
// an entry whose agent is connected right now is never taken.
func TestFirstMachineIDAdoptsTheExistingEntry(t *testing.T) {
	client, fs, _ := machineTestServer(t)
	old := registerAs(t, client, "box", "", "ins_old", nil)
	fs.mu.Lock()
	fs.nodes[old] = withConnected(fs.nodes[old], false)
	fs.mu.Unlock()

	got := registerAs(t, client, "box", "mch_A", "ins_new", nil)
	if got != old {
		t.Fatalf("the existing entry %s was not adopted (got %s)", old, got)
	}
	if fs.nodes[old].MachineID != "mch_A" || len(nodesNow(fs)) != 1 {
		t.Fatalf("adoption must stamp the machine id on the entry, no duplicate: %+v", fs.nodes)
	}

	// An entry that is connected (another live agent with that hostname) is left alone.
	other := registerAs(t, client, "twin", "", "ins_t", nil)
	time.Sleep(150 * time.Millisecond) // let the server notice the helper's closed stream first
	fs.mu.Lock()
	fs.nodes[other] = withConnected(fs.nodes[other], true) // the helper's stream has closed; make it look live again
	fs.mu.Unlock()
	third := registerAs(t, client, "twin", "mch_T", "ins_t2", nil)
	if third == other {
		t.Fatal("took over an entry whose agent is connected")
	}
}

// nodesNow copies the fake store's nodes under its lock (the server writes to it from its own goroutines).
func nodesNow(fs *fakeStore) map[string]store.Node {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make(map[string]store.Node, len(fs.nodes))
	for k, v := range fs.nodes {
		out[k] = v
	}
	return out
}

func withConnected(n store.Node, c bool) store.Node { n.Connected = c; return n }
