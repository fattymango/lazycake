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
	mu     sync.Mutex
	tokens map[string]store.APIToken
	nodes  map[string]store.Node
	hbs    map[string]time.Time
	logs   []store.LogLine
	images map[string][]store.CachedImage
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tokens: map[string]store.APIToken{},
		nodes:  map[string]store.Node{},
		hbs:    map[string]time.Time{},
		images: map[string][]store.CachedImage{},
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

// --- unimplemented-by-design for this package's tests ---

func (f *fakeStore) CreateAccount(context.Context, store.Account) error { panic("not used") }
func (f *fakeStore) GetAccount(context.Context, string) (store.Account, error) {
	panic("not used")
}
func (f *fakeStore) AdjustBalance(context.Context, string, int64) (int64, error) {
	panic("not used")
}
func (f *fakeStore) CreateToken(context.Context, store.APIToken) error { panic("not used") }
func (f *fakeStore) ListNodes(context.Context) ([]store.Node, error)   { panic("not used") }
func (f *fakeStore) SetNodeBenchScore(context.Context, string, float64) error {
	panic("not used")
}
func (f *fakeStore) SetNodeTrustScore(context.Context, string, float64) error {
	panic("not used")
}
func (f *fakeStore) CreateTask(context.Context, store.Task) error { panic("not used") }
func (f *fakeStore) GetTask(context.Context, string) (store.Task, error) {
	panic("not used")
}
func (f *fakeStore) ListTasksByNode(context.Context, string, []store.TaskState) ([]store.Task, error) {
	panic("not used")
}
func (f *fakeStore) ClaimQueuedTask(context.Context, string, store.CapacityFilter, time.Time) (store.Task, error) {
	panic("not used")
}
func (f *fakeStore) TransitionTask(context.Context, string, []store.TaskState, store.TaskState, store.TaskUpdate) error {
	panic("not used")
}
func (f *fakeStore) RequeueOverdue(context.Context, time.Time) ([]store.Task, error) {
	panic("not used")
}
func (f *fakeStore) ListLogs(context.Context, string, int64) ([]store.LogLine, error) {
	panic("not used")
}
func (f *fakeStore) RemoveCachedImages(context.Context, string, []string) error { panic("not used") }
func (f *fakeStore) ListCachedImages(context.Context, string) ([]store.CachedImage, error) {
	panic("not used")
}
func (f *fakeStore) NodesWithImage(context.Context, string) ([]string, error) { panic("not used") }

var _ store.Store = (*fakeStore)(nil)
