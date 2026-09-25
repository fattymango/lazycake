//go:build integration

package portalapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// testStore connects to LAZYCAKE_TEST_DATABASE_URL, same convention as
// internal/coordinator/store's own integration tests, and truncates every
// table this package's handlers touch so each test starts clean.
func testStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	url := os.Getenv("LAZYCAKE_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable"
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, url)
	require.NoError(t, err)
	t.Cleanup(s.Close)

	_, err = s.Pool().Exec(ctx, `TRUNCATE task_logs, node_images, sessions, portal_credentials,
		ledger_entries, task_holds, task_meters, gateways, tasks, nodes, api_tokens, accounts CASCADE`)
	require.NoError(t, err)
	return s
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testClient wraps an httptest.Server-backed portalapi.Server with a
// cookie-jar http.Client - the same session-cookie flow a real browser
// goes through (signup/login sets it, every later request sends it back).
type testClient struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func newTestServer(t *testing.T) (*testClient, store.Store, *events.Bus) {
	t.Helper()
	st := testStore(t)
	bus := events.NewBus()
	srv := &Server{
		Store:    st,
		Customer: &api.CustomerServer{Store: st, Rates: pricing.DefaultRates(), Bus: bus},
		Bus:      bus,
		Log:      discardLog(),
		Dev:      true,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &testClient{t: t, base: ts.URL, hc: &http.Client{Jar: jar}}, st, bus
}

func (c *testClient) do(method, path string, body any) *http.Response {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	require.NoError(c.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	require.NoError(c.t, err)
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
}

// --- Auth: signup, login, logout, lockout ---

func TestSignupLoginLogout(t *testing.T) {
	c, _, _ := newTestServer(t)

	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: "alice", Password: "hunter22"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	signed := decodeBody[authResponse](t, resp)
	require.Equal(t, "alice", signed.Username)
	require.Equal(t, store.RoleCustomer, signed.Role)

	// Signup lands signed in: /me works immediately, no separate login.
	resp = c.do(http.MethodGet, "/api/portal/customer/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	me := decodeBody[meResponse](t, resp)
	require.Equal(t, signed.AccountID, me.AccountID)
	require.Equal(t, int64(0), me.BalanceMicros)

	resp = c.do(http.MethodPost, "/api/portal/logout", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Protected route now rejects.
	resp = c.do(http.MethodGet, "/api/portal/customer/me", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Login with the same credentials signs back in.
	resp = c.do(http.MethodPost, "/api/portal/customer/login", credentialsRequest{Username: "alice", Password: "hunter22"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp = c.do(http.MethodGet, "/api/portal/customer/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSignupRejectsShortPassword(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: "bob", Password: "short"})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSignupRejectsDuplicateUsername(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: "carol", Password: "password1"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	resp = c.do(http.MethodPost, "/api/portal/provider/signup", credentialsRequest{Username: "carol", Password: "password2"})
	require.Equal(t, http.StatusConflict, resp.StatusCode, "a username is global across both portals, not per-role")
}

func TestLoginRejectsWrongPasswordAndUnknownUsernameIdentically(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: "dave", Password: "correcthorse"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// A fresh client with no session, so login below is the only thing
	// under test - the signup call above already left c itself signed in.
	fresh := &testClient{t: t, base: c.base, hc: mustJarClient(t)}

	wrongPassResp := fresh.do(http.MethodPost, "/api/portal/customer/login", credentialsRequest{Username: "dave", Password: "wrong"})
	unknownUserResp := fresh.do(http.MethodPost, "/api/portal/customer/login", credentialsRequest{Username: "nobody-registered", Password: "whatever1"})

	require.Equal(t, http.StatusUnauthorized, wrongPassResp.StatusCode)
	require.Equal(t, http.StatusUnauthorized, unknownUserResp.StatusCode)

	wrongBody := decodeBody[errorBody](t, wrongPassResp)
	unknownBody := decodeBody[errorBody](t, unknownUserResp)
	require.Equal(t, wrongBody.Error, unknownBody.Error, "must not be distinguishable from the response alone")
}

func mustJarClient(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &http.Client{Jar: jar}
}

func TestLoginLockoutAfterRepeatedFailures(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: "eve", Password: "correcthorse"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()
	c.do(http.MethodPost, "/api/portal/logout", nil).Body.Close()

	for i := 0; i < maxFailedAttempts; i++ {
		resp := c.do(http.MethodPost, "/api/portal/customer/login", credentialsRequest{Username: "eve", Password: "wrong"})
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		resp.Body.Close()
	}

	// Even the *correct* password is now rejected while locked out.
	resp = c.do(http.MethodPost, "/api/portal/customer/login", credentialsRequest{Username: "eve", Password: "correcthorse"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "must be locked out after repeated failures")
}

func TestCrossRoleSessionRejected(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: "frank", Password: "password1"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// A customer session must not authorize a provider-only endpoint.
	resp = c.do(http.MethodGet, "/api/portal/provider/me", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestLogoutIsIdempotentWithNoSession(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/logout", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// --- Customer: tasks, gateways, ledger, balance ---

func signUpCustomer(t *testing.T, c *testClient, username string) authResponse {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/portal/customer/signup", credentialsRequest{Username: username, Password: "password1"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return decodeBody[authResponse](t, resp)
}

func signUpProvider(t *testing.T, c *testClient, username string) authResponse {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/portal/provider/signup", credentialsRequest{Username: username, Password: "password1"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return decodeBody[authResponse](t, resp)
}

func TestSubmitTaskRejectsNonDigestPinnedImage(t *testing.T) {
	c, _, _ := newTestServer(t)
	signUpCustomer(t, c, "gina")

	resp := c.do(http.MethodPost, "/api/portal/customer/tasks", submitTaskRequest{
		Image: "alpine:latest", Cores: 1, MemoryMB: 256, WallTimeoutS: 30,
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSubmitTaskInsufficientBalance(t *testing.T) {
	c, _, _ := newTestServer(t)
	signUpCustomer(t, c, "hank") // zero balance by default

	resp := c.do(http.MethodPost, "/api/portal/customer/tasks", submitTaskRequest{
		Image: "alpine@sha256:abc", Cores: 8, MemoryMB: 16384, WallTimeoutS: 36000,
	})
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestSubmitTaskListAndGet(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpCustomer(t, c, "ivan")
	fundAccount(t, st, signed.AccountID, 10_000_000)

	resp := c.do(http.MethodPost, "/api/portal/customer/tasks", submitTaskRequest{
		Image: "alpine@sha256:abc", Cores: 1, MemoryMB: 256, WallTimeoutS: 30,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeBody[taskView](t, resp)
	require.Equal(t, "queued", created.State)
	require.NotEmpty(t, created.ID)

	resp = c.do(http.MethodGet, "/api/portal/customer/tasks", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[[]taskView](t, resp)
	require.Len(t, list, 1)
	require.Equal(t, created.ID, list[0].ID)

	resp = c.do(http.MethodGet, "/api/portal/customer/tasks/"+created.ID, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decodeBody[taskView](t, resp)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "alpine@sha256:abc", got.Image)
}

func TestTaskIsolationBetweenAccounts(t *testing.T) {
	c1, st, _ := newTestServer(t)
	signed1 := signUpCustomer(t, c1, "julia")
	fundAccount(t, st, signed1.AccountID, 10_000_000)
	resp := c1.do(http.MethodPost, "/api/portal/customer/tasks", submitTaskRequest{
		Image: "alpine@sha256:abc", Cores: 1, MemoryMB: 256, WallTimeoutS: 30,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	task := decodeBody[taskView](t, resp)

	// A second, unrelated signed-in customer on the same server must not
	// be able to see the first account's task.
	c2 := &testClient{t: t, base: c1.base, hc: mustJarClient(t)}
	signUpCustomer(t, c2, "karl")

	resp = c2.do(http.MethodGet, "/api/portal/customer/tasks/"+task.ID, nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = c2.do(http.MethodGet, "/api/portal/customer/tasks", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[[]taskView](t, resp)
	require.Empty(t, list, "must only ever see its own tasks")
}

func TestGatewayCreateAndList(t *testing.T) {
	c, _, _ := newTestServer(t)
	signUpCustomer(t, c, "liam")

	resp := c.do(http.MethodPost, "/api/portal/customer/gateways", createGatewayRequest{
		Label: "home-lab", Services: []gatewayServiceView{{Name: "db", Port: 5432}},
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeBody[gatewayView](t, resp)
	require.Equal(t, "home-lab", created.Label)
	require.False(t, created.Connected)

	resp = c.do(http.MethodGet, "/api/portal/customer/gateways", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[[]gatewayView](t, resp)
	require.Len(t, list, 1)
	require.Equal(t, created.ID, list[0].ID)
}

func TestGatewayCreateRejectsMissingLabel(t *testing.T) {
	c, _, _ := newTestServer(t)
	signUpCustomer(t, c, "mona")
	resp := c.do(http.MethodPost, "/api/portal/customer/gateways", createGatewayRequest{})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAddBalanceAndLedger(t *testing.T) {
	c, _, _ := newTestServer(t)
	signUpCustomer(t, c, "nora")

	resp := c.do(http.MethodPost, "/api/portal/customer/balance/add", addBalanceRequest{AmountMicros: 5_000_000})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp = c.do(http.MethodGet, "/api/portal/customer/me", nil)
	me := decodeBody[meResponse](t, resp)
	require.Equal(t, int64(5_000_000), me.BalanceMicros)
	require.Equal(t, int64(5_000_000), me.AvailableBalanceMicros)

	resp = c.do(http.MethodPost, "/api/portal/customer/balance/add", addBalanceRequest{AmountMicros: -1})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "must reject a non-positive amount")
}

// --- Provider: nodes, install token, ledger ---

func TestProviderNodesScoping(t *testing.T) {
	c1, st, _ := newTestServer(t)
	signed1 := signUpProvider(t, c1, "oscar")
	mustNode(t, st, "nod_1", signed1.AccountID)

	c2 := &testClient{t: t, base: c1.base, hc: mustJarClient(t)}
	signed2 := signUpProvider(t, c2, "paula")
	mustNode(t, st, "nod_2", signed2.AccountID)

	resp := c1.do(http.MethodGet, "/api/portal/provider/nodes", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[[]nodeView](t, resp)
	require.Len(t, list, 1)
	require.Equal(t, "nod_1", list[0].ID)

	// oscar must not be able to fetch paula's node by ID either.
	resp = c1.do(http.MethodGet, "/api/portal/provider/nodes/nod_2", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = c1.do(http.MethodGet, "/api/portal/provider/nodes/nod_1", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestProviderInstallTokenAuthenticatesAsAgent(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpProvider(t, c, "quinn")

	resp := c.do(http.MethodPost, "/api/portal/provider/nodes/install-token", nil)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	got := decodeBody[installTokenResponse](t, resp)
	require.NotEmpty(t, got.Token)
	require.Contains(t, got.InstallCommand, got.Token)

	tok, err := st.Authenticate(context.Background(), auth.Hash(got.Token))
	require.NoError(t, err)
	require.Equal(t, signed.AccountID, tok.AccountID)
	require.Equal(t, store.TokenAgent, tok.Kind)
}

func TestProviderMeReflectsLifetimeEarnings(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpProvider(t, c, "rosa")
	require.NoError(t, st.CreateAccount(context.Background(), store.Account{ID: "act_customer_x", Name: "customer-x", BalanceMicros: 1_000_000}))
	require.NoError(t, st.CreateTask(context.Background(), store.Task{
		ID: "tsk_settled", AccountID: "act_customer_x", State: store.TaskSucceeded,
		Image: "alpine@sha256:a", Limits: store.Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))
	require.NoError(t, st.SettleTask(context.Background(), "tsk_settled", "act_customer_x", signed.AccountID, 250_000))

	resp := c.do(http.MethodGet, "/api/portal/provider/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	me := decodeBody[providerMeResponse](t, resp)
	require.Equal(t, int64(250_000), me.LifetimeEarningsMicros)

	resp = c.do(http.MethodGet, "/api/portal/provider/ledger", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	ledger := decodeBody[[]ledgerView](t, resp)
	require.Len(t, ledger, 1)
	require.Equal(t, "credit", ledger[0].Kind)
	require.Equal(t, int64(250_000), ledger[0].AmountMicros)
}

// --- Account-scoped SSE ---

func TestAccountEventsAreScopedToOwnAccount(t *testing.T) {
	c, _, bus := newTestServer(t)
	signed := signUpCustomer(t, c, "sam")

	req, err := http.NewRequest(http.MethodGet, c.base+"/api/portal/customer/events", nil)
	require.NoError(t, err)
	resp, err := c.hc.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	reader := bufio.NewReader(resp.Body)
	time.Sleep(50 * time.Millisecond) // let the handler subscribe before publishing

	bus.Publish(events.Event{Type: "task_state", AccountID: "act_someone_else", TaskID: "not-mine", State: "queued"})
	bus.Publish(events.Event{Type: "task_state", AccountID: signed.AccountID, TaskID: "mine", State: "queued"})

	line := readSSEDataLine(t, reader)
	var ev eventView
	require.NoError(t, json.Unmarshal([]byte(line), &ev))
	require.Equal(t, "mine", ev.TaskID, "must never receive another account's event")
}

func readSSEDataLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	lineCh := make(chan string, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				lineCh <- strings.TrimPrefix(strings.TrimRight(line, "\n"), "data: ")
				return
			}
		}
	}()
	select {
	case line := <-lineCh:
		return line
	case <-deadline:
		t.Fatal("timed out waiting for an SSE data line")
		return ""
	}
}

// --- helpers shared across this file ---

func fundAccount(t *testing.T, st store.Store, accountID string, micros int64) {
	t.Helper()
	_, err := st.AdjustBalance(context.Background(), accountID, micros)
	require.NoError(t, err)
}

func mustNode(t *testing.T, st store.Store, id, accountID string) {
	t.Helper()
	require.NoError(t, st.UpsertNode(context.Background(), store.Node{
		ID: id, AccountID: accountID, Hostname: "h", Arch: "amd64",
		OfferCores: 4, OfferMemoryMB: 8192, OfferDiskMB: 20000,
	}))
}
