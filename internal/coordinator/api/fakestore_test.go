package api

import (
	"context"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// fakeStore is a minimal in-memory store.Store for api package tests. It
// only implements what Server actually calls; anything else panics so a
// test relying on unimplemented behaviour fails loudly instead of silently
// returning zero values.
type fakeStore struct {
	mu       sync.Mutex
	accounts map[string]store.Account
	tokens   map[string]store.APIToken
	nodes    map[string]store.Node
	hbs      map[string]time.Time
	logs     []store.LogLine
	images   map[string][]store.CachedImage
	tasks    map[string]store.Task
	idemKeys map[[2]string]string // (account_id, idempotency_key) -> task_id
	gateways map[string]store.Gateway
	meters   map[string]store.TaskMeter
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts: map[string]store.Account{},
		tokens:   map[string]store.APIToken{},
		gateways: map[string]store.Gateway{},
		nodes:    map[string]store.Node{},
		hbs:      map[string]time.Time{},
		images:   map[string][]store.CachedImage{},
		tasks:    map[string]store.Task{},
		idemKeys: map[[2]string]string{},
		meters:   map[string]store.TaskMeter{},
	}
}

func (f *fakeStore) addToken(hash string, tok store.APIToken) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[hash] = tok
}

func (f *fakeStore) Authenticate(ctx context.Context, tokenHash []byte) (store.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok, ok := f.tokens[string(tokenHash)]
	if !ok {
		return store.APIToken{}, store.ErrNotFound
	}
	return tok, nil
}

func (f *fakeStore) UpsertNode(ctx context.Context, n store.Node) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n.Connected = true
	f.nodes[n.ID] = n
	return nil
}

func (f *fakeStore) GetNode(ctx context.Context, id string) (store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[id]
	if !ok {
		return store.Node{}, store.ErrNotFound
	}
	return n, nil
}

func (f *fakeStore) GetNodeByInstanceID(ctx context.Context, accountID, instanceID string) (store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.AccountID == accountID && n.InstanceID == instanceID && instanceID != "" {
			return n, nil
		}
	}
	return store.Node{}, store.ErrNotFound
}

func (f *fakeStore) SetNodeConnected(ctx context.Context, id string, connected bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[id]
	n.Connected = connected
	f.nodes[id] = n
	return nil
}

func (f *fakeStore) RecordHeartbeat(ctx context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hbs[id] = at
	return nil
}

func (f *fakeStore) SetNodeOffer(ctx context.Context, id string, cores float64, memoryMB, diskMB int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[id]
	n.OfferCores, n.OfferMemoryMB, n.OfferDiskMB = cores, memoryMB, diskMB
	f.nodes[id] = n
	return nil
}

func (f *fakeStore) RecordCachedImages(ctx context.Context, nodeID string, images []store.CachedImage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.images[nodeID] = append(f.images[nodeID], images...)
	return nil
}

func (f *fakeStore) AppendLogs(ctx context.Context, lines []store.LogLine) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, lines...)
	return nil
}

func (f *fakeStore) CreateAccount(ctx context.Context, a store.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts[a.ID] = a
	return nil
}

func (f *fakeStore) GetAccount(ctx context.Context, id string) (store.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) AdjustBalance(ctx context.Context, id string, delta int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return 0, store.ErrNotFound
	}
	a.BalanceMicros += delta
	f.accounts[id] = a
	return a.BalanceMicros, nil
}

func (f *fakeStore) CreateToken(ctx context.Context, t store.APIToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[string(t.TokenHash)] = t
	return nil
}

func (f *fakeStore) ListNodes(ctx context.Context) ([]store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, n)
	}
	return out, nil
}

func (f *fakeStore) SetNodeBenchScore(ctx context.Context, id string, score float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[id]
	n.BenchScore = &score
	f.nodes[id] = n
	return nil
}

func (f *fakeStore) SetNodeTrustScore(ctx context.Context, id string, score float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[id]
	n.TrustScore = score
	f.nodes[id] = n
	return nil
}

func (f *fakeStore) CreateTask(ctx context.Context, t store.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.IdempotencyKey != nil {
		key := [2]string{t.AccountID, *t.IdempotencyKey}
		if _, exists := f.idemKeys[key]; exists {
			return store.ErrDuplicate
		}
		f.idemKeys[key] = t.ID
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	f.tasks[t.ID] = t
	return nil
}

func (f *fakeStore) GetTask(ctx context.Context, id string) (store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return store.Task{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) ListTasksByNode(ctx context.Context, nodeID string, states []store.TaskState) ([]store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[store.TaskState]bool{}
	for _, s := range states {
		want[s] = true
	}
	var out []store.Task
	for _, t := range f.tasks {
		if t.NodeID != nil && *t.NodeID == nodeID && want[t.State] {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeStore) ClaimQueuedTask(context.Context, string, store.CapacityFilter, time.Time) (store.Task, error) {
	panic("not used")
}

func (f *fakeStore) TransitionTask(ctx context.Context, id string, from []store.TaskState, to store.TaskState, upd store.TaskUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return store.ErrNotFound
	}
	matched := false
	for _, s := range from {
		if t.State == s {
			matched = true
		}
	}
	if !matched {
		return store.ErrConflict
	}
	t.State = to
	if upd.NodeID != nil {
		t.NodeID = upd.NodeID
	}
	if upd.ExitCode != nil {
		t.ExitCode = upd.ExitCode
	}
	if upd.ExitReason != nil {
		t.ExitReason = upd.ExitReason
	}
	if upd.StartedAt != nil {
		t.StartedAt = upd.StartedAt
	}
	if upd.FinishedAt != nil {
		t.FinishedAt = upd.FinishedAt
	}
	f.tasks[id] = t
	return nil
}

func (f *fakeStore) RequeueOverdue(context.Context, time.Time) ([]store.Task, error) {
	panic("not used")
}

func (f *fakeStore) RequeueTaskForRetry(ctx context.Context, id string, fromStates []store.TaskState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return store.ErrNotFound
	}
	matched := false
	for _, s := range fromStates {
		if t.State == s {
			matched = true
		}
	}
	if !matched {
		return store.ErrConflict
	}
	t.State = store.TaskQueued
	t.NodeID = nil
	t.LeaseExpiresAt = nil
	t.RequeueAfter = nil
	t.Attempt++
	f.tasks[id] = t
	return nil
}

func (f *fakeStore) AbandonTask(ctx context.Context, id string, fromStates []store.TaskState, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return store.ErrNotFound
	}
	matched := false
	for _, s := range fromStates {
		if t.State == s {
			matched = true
		}
	}
	if !matched {
		return store.ErrConflict
	}
	t.State = store.TaskAbandoned
	t.FinishedAt = &at
	f.tasks[id] = t
	return nil
}

func (f *fakeStore) ExtendNodeRequeue(ctx context.Context, nodeID string, newRequeueAfter time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, t := range f.tasks {
		if t.NodeID != nil && *t.NodeID == nodeID && (t.State == store.TaskDispatched || t.State == store.TaskRunning) {
			if t.RequeueAfter == nil || t.RequeueAfter.Before(newRequeueAfter) {
				t.RequeueAfter = &newRequeueAfter
				f.tasks[id] = t
			}
		}
	}
	return nil
}

func (f *fakeStore) ListLogs(ctx context.Context, taskID string, sinceSeq int64) ([]store.LogLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.LogLine
	for _, l := range f.logs {
		if l.TaskID == taskID && l.Seq > sinceSeq {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeStore) RemoveCachedImages(context.Context, string, []string) error { panic("not used") }
func (f *fakeStore) ListCachedImages(context.Context, string) ([]store.CachedImage, error) {
	panic("not used")
}
func (f *fakeStore) NodesWithImage(context.Context, string) ([]string, error) { panic("not used") }

func (f *fakeStore) CreateGateway(ctx context.Context, g store.Gateway) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gateways[g.ID] = g
	return nil
}

func (f *fakeStore) GetGateway(ctx context.Context, id string) (store.Gateway, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.gateways[id]
	if !ok {
		return store.Gateway{}, store.ErrNotFound
	}
	return g, nil
}

func (f *fakeStore) ListGatewaysByAccount(ctx context.Context, accountID string) ([]store.Gateway, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Gateway
	for _, g := range f.gateways {
		if g.AccountID == accountID {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeStore) SetGatewayConnected(ctx context.Context, id string, connected bool, noisePubkey []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.gateways[id]
	if !ok {
		return store.ErrNotFound
	}
	g.Connected = connected
	if connected && len(noisePubkey) > 0 {
		g.NoisePubkey = noisePubkey
	}
	f.gateways[id] = g
	return nil
}

func (f *fakeStore) RecordMeterStarted(ctx context.Context, taskID, nodeID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.meters[taskID]; ok {
		return nil
	}
	f.meters[taskID] = store.TaskMeter{TaskID: taskID, NodeID: nodeID, StartedAt: at}
	return nil
}

func (f *fakeStore) RecordMeterFinished(ctx context.Context, taskID string, at time.Time, durationS, normalisedS float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.meters[taskID]
	if !ok {
		return store.ErrNotFound
	}
	m.FinishedAt = &at
	m.DurationS = &durationS
	m.NormalisedS = &normalisedS
	f.meters[taskID] = m
	return nil
}

func (f *fakeStore) GetMeter(ctx context.Context, taskID string) (store.TaskMeter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.meters[taskID]
	if !ok {
		return store.TaskMeter{}, store.ErrNotFound
	}
	return m, nil
}

var _ store.Store = (*fakeStore)(nil)
