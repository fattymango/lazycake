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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
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
		ledger_entries, task_holds, task_meters, node_usage, node_task_usage, node_usage_latest, task_usage_totals, gateway_traffic, gateway_totals, task_gateway_totals, gateways, tasks, nodes, api_tokens, accounts CASCADE`)
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

// TestUnifiedLoginDiscoversRole covers the account-already-knows-its-role
// login path (POST /api/portal/login, no role in the URL): a provider
// account logging in through it must land a provider session usable
// against provider-only routes, and a customer account the same for
// customer-only routes - proving the discovered role, not a guess or a
// default, drives the session.
func TestUnifiedLoginDiscoversRole(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/provider/signup", credentialsRequest{Username: "uma", Password: "hunter22"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, http.StatusNoContent, c.do(http.MethodPost, "/api/portal/logout", nil).StatusCode)

	resp = c.do(http.MethodPost, "/api/portal/login", credentialsRequest{Username: "uma", Password: "hunter22"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	signed := decodeBody[authResponse](t, resp)
	require.Equal(t, store.RoleProvider, signed.Role)

	// The session this created must actually work against the provider
	// portal, not just report the right role in the login response.
	resp = c.do(http.MethodGet, "/api/portal/provider/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// ...and must not pass as a customer session.
	resp = c.do(http.MethodGet, "/api/portal/customer/me", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUnifiedLoginGenericErrorOnFailure(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodPost, "/api/portal/login", credentialsRequest{Username: "nobody", Password: "whatever1"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestWhoami covers GET /api/portal/me: it must work for either role's
// session (that's the entire point - the frontend calls it before it
// knows which portal to render) and report the signed-in account's real
// username and role.
func TestWhoami(t *testing.T) {
	c, _, _ := newTestServer(t)
	signed := signUpCustomer(t, c, "vic")

	resp := c.do(http.MethodGet, "/api/portal/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	who := decodeBody[authResponse](t, resp)
	require.Equal(t, signed.AccountID, who.AccountID)
	require.Equal(t, "vic", who.Username)
	require.Equal(t, store.RoleCustomer, who.Role)
}

func TestWhoamiRejectsNoSession(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodGet, "/api/portal/me", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
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
	created := decodeBody[createGatewayResponse](t, resp)
	require.Equal(t, "home-lab", created.Label)
	require.False(t, created.Connected)

	resp = c.do(http.MethodGet, "/api/portal/customer/gateways", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[[]gatewayView](t, resp)
	require.Len(t, list, 1)
	require.Equal(t, created.ID, list[0].ID)
}

// TestGatewayCreateMintsWorkingInstallToken is the regression test for a
// real gap caught live: handleCreateGateway created the Gateway row but
// never minted the install token a real gateway process needs to
// authenticate at all (unlike the gRPC CreateGateway it was meant to
// mirror) - a gateway made through the portal could never actually run.
func TestGatewayCreateMintsWorkingInstallToken(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpCustomer(t, c, "morgan")

	resp := c.do(http.MethodPost, "/api/portal/customer/gateways", createGatewayRequest{
		Label: "home-lab", Services: []gatewayServiceView{{Name: "db", Port: 5432}},
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeBody[createGatewayResponse](t, resp)
	require.NotEmpty(t, created.InstallToken, "install token must be minted, not just the gateway row")

	tok, err := st.Authenticate(context.Background(), auth.Hash(created.InstallToken))
	require.NoError(t, err)
	require.Equal(t, signed.AccountID, tok.AccountID)
	require.Equal(t, store.TokenGateway, tok.Kind)
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

// TestDeleteNodeRemovesIt covers the machine-management gap this
// session's live testing surfaced: there was no way to remove a machine
// at all. Also proves account scoping applies here too - one account
// can't delete another's node.
func TestDeleteNodeRemovesIt(t *testing.T) {
	c1, st, _ := newTestServer(t)
	signed1 := signUpProvider(t, c1, "walt")
	mustNode(t, st, "nod_mine", signed1.AccountID)

	c2 := &testClient{t: t, base: c1.base, hc: mustJarClient(t)}
	signed2 := signUpProvider(t, c2, "xena")
	mustNode(t, st, "nod_theirs", signed2.AccountID)

	// Can't delete another account's node - 404, not 403 (never reveal it
	// exists), and it must still be there afterward.
	resp := c1.do(http.MethodDelete, "/api/portal/provider/nodes/nod_theirs", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	_, err := st.GetNode(context.Background(), "nod_theirs")
	require.NoError(t, err)

	resp = c1.do(http.MethodDelete, "/api/portal/provider/nodes/nod_mine", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	_, err = st.GetNode(context.Background(), "nod_mine")
	require.ErrorIs(t, err, store.ErrNotFound)

	// Gone from the list too, not just individually 404ing.
	resp = c1.do(http.MethodGet, "/api/portal/provider/nodes", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, decodeBody[[]nodeView](t, resp))
}

// TestDeleteNodeDoesNotBreakOnTaskHistory proves
// migrations/014_tasks_node_id_delete_set_null.sql's whole point: a node
// that has actually run tasks can still be deleted (the FK used to be
// RESTRICT, which would fail this with a constraint violation), and the
// task's own row survives with node_id cleared rather than being deleted
// itself - task history outlives the node that produced it. Also covers
// migrations/015_task_meters_node_id_delete_set_null.sql: task_meters had
// the exact same RESTRICT problem, just missed the first time around
// (this test only created a tasks row, never a task_meters one) - caught
// live deleting a real node with real task history, which does always
// have a meter row.
func TestDeleteNodeDoesNotBreakOnTaskHistory(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpProvider(t, c, "yara")
	mustNode(t, st, "nod_worked", signed.AccountID)
	require.NoError(t, st.CreateAccount(context.Background(), store.Account{ID: "act_cust_y", Name: "cust-y"}))
	require.NoError(t, st.CreateTask(context.Background(), store.Task{
		ID: "tsk_ran_here", AccountID: "act_cust_y", State: store.TaskQueued,
		Image: "alpine@sha256:a", Limits: store.Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))
	nodeID := "nod_worked"
	require.NoError(t, st.TransitionTask(context.Background(), "tsk_ran_here",
		[]store.TaskState{store.TaskQueued}, store.TaskSucceeded, store.TaskUpdate{NodeID: &nodeID}))
	require.NoError(t, st.RecordMeterStarted(context.Background(), "tsk_ran_here", nodeID, time.Now()))
	require.NoError(t, st.RecordMeterFinished(context.Background(), "tsk_ran_here", time.Now(), 12.5, 12.5))

	resp := c.do(http.MethodDelete, "/api/portal/provider/nodes/nod_worked", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	task, err := st.GetTask(context.Background(), "tsk_ran_here")
	require.NoError(t, err)
	require.Nil(t, task.NodeID, "task's node_id should be cleared, not the task deleted or the delete blocked")

	meter, err := st.GetMeter(context.Background(), "tsk_ran_here")
	require.NoError(t, err)
	require.Nil(t, meter.NodeID, "meter's node_id should be cleared, not the meter row deleted or the delete blocked")
	require.NotNil(t, meter.NormalisedS, "the actual billing data must survive the node's deletion")
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

// --- Task log stream (phase 8, task 8.2) ---

// failedTaskWithLogs submits a task, appends n log lines to it and marks it
// failed, returning its ID. The stream endpoint under test reads only the
// store, so this stands in for an agent having run and died.
func failedTaskWithLogs(t *testing.T, c *testClient, st store.Store, n int) string {
	t.Helper()
	signed := signUpCustomer(t, c, "logger")
	fundAccount(t, st, signed.AccountID, 10_000_000)
	resp := c.do(http.MethodPost, "/api/portal/customer/tasks", submitTaskRequest{
		Image: "alpine@sha256:abc", Cores: 1, MemoryMB: 256, WallTimeoutS: 30,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	id := decodeBody[taskView](t, resp).ID

	ctx := context.Background()
	lines := make([]store.LogLine, n)
	for i := range lines {
		lines[i] = store.LogLine{TaskID: id, Seq: int64(i + 1), Stream: "stderr", At: time.Now(), Line: "boom " + strconv.Itoa(i+1)}
	}
	require.NoError(t, st.AppendLogs(ctx, lines))
	reason := "error"
	require.NoError(t, st.TransitionTask(ctx, id, []store.TaskState{store.TaskQueued}, store.TaskFailed, store.TaskUpdate{ExitReason: &reason}))
	return id
}

// sseFrames reads an SSE response to EOF (or fails if it doesn't end), and
// returns each frame's id, event name and data.
type sseFrame struct{ id, event, data string }

func readSSEFrames(t *testing.T, resp *http.Response) []sseFrame {
	t.Helper()
	defer resp.Body.Close()
	type result struct {
		frames []sseFrame
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var frames []sseFrame
		var cur sseFrame
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur != (sseFrame{}) {
					frames = append(frames, cur)
					cur = sseFrame{}
				}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
		done <- result{frames, sc.Err()}
	}()
	select {
	case r := <-done:
		return r.frames
	case <-time.After(8 * time.Second):
		t.Fatal("the log stream of a finished task never ended")
		return nil
	}
}

// The bug: a finished task's stream closed with no signal, the browser's
// EventSource reconnected, and the whole log replayed again - forever.
func TestLogStreamOfFinishedTaskEndsWithAnEndEvent(t *testing.T) {
	c, st, _ := newTestServer(t)
	id := failedTaskWithLogs(t, c, st, 3)

	frames := readSSEFrames(t, c.do(http.MethodGet, "/api/portal/customer/tasks/"+id+"/logs", nil))
	require.Len(t, frames, 4, "3 log lines then the end event: %+v", frames)
	for i, f := range frames[:3] {
		require.Equal(t, strconv.Itoa(i+1), f.id, "every line carries its seq as the SSE id")
		require.Empty(t, f.event)
		require.Contains(t, f.data, "boom "+strconv.Itoa(i+1))
	}
	require.Equal(t, "end", frames[3].event, "the stream says it is done instead of just closing")
}

// A reconnect (network blip, laptop sleep) resumes after the last line seen.
func TestLogStreamResumesFromLastEventID(t *testing.T) {
	c, st, _ := newTestServer(t)
	id := failedTaskWithLogs(t, c, st, 5)

	req, err := http.NewRequest(http.MethodGet, c.base+"/api/portal/customer/tasks/"+id+"/logs", nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "3")
	resp, err := c.hc.Do(req)
	require.NoError(t, err)

	frames := readSSEFrames(t, resp)
	require.Len(t, frames, 3, "lines 4 and 5 then end, never a replay: %+v", frames)
	require.Equal(t, "4", frames[0].id)
	require.Equal(t, "5", frames[1].id)
	require.Equal(t, "end", frames[2].event)

	// Resuming from the very last line yields nothing but the end.
	req.Header.Set("Last-Event-ID", "5")
	resp, err = c.hc.Do(req)
	require.NoError(t, err)
	frames = readSSEFrames(t, resp)
	require.Len(t, frames, 1)
	require.Equal(t, "end", frames[0].event)
}

// An unknown endpoint under /api/portal/ must answer in the API's own JSON
// error shape (the frontend parses it), not net/http's plain-text 404.
func TestUnknownPortalPathIsJSON404(t *testing.T) {
	c, _, _ := newTestServer(t)
	resp := c.do(http.MethodGet, "/api/portal/definitely/not/a/route", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "json")
	body := decodeBody[map[string]string](t, resp)
	require.NotEmpty(t, body["error"])
}

// --- Stop a task (phase 8, task 8.12) ---

func submitTaskForTest(t *testing.T, c *testClient) string {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/portal/customer/tasks", submitTaskRequest{Image: "alpine@sha256:abc", Cores: 1, MemoryMB: 256, WallTimeoutS: 30})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return decodeBody[taskView](t, resp).ID
}

func TestStopQueuedTaskIsImmediateFreeAndRepeatable(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpCustomer(t, c, "stopper")
	fundAccount(t, st, signed.AccountID, 10_000_000)
	id := submitTaskForTest(t, c)

	resp := c.do(http.MethodPost, "/api/portal/customer/tasks/"+id+"/cancel", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decodeBody[taskView](t, resp)
	require.Equal(t, "cancelled", got.State)
	require.Equal(t, "stopped", got.ExitReason)
	require.False(t, got.CancelRequested, "a finished task isn't 'stopping'")

	me := decodeBody[meResponse](t, c.do(http.MethodGet, "/api/portal/customer/me", nil))
	require.Equal(t, int64(10_000_000), me.BalanceMicros, "a task that never ran costs nothing")
	require.Equal(t, int64(10_000_000), me.AvailableBalanceMicros)

	resp = c.do(http.MethodPost, "/api/portal/customer/tasks/"+id+"/cancel", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "stopping it again is a harmless repeat")
}

func TestStopRunningTaskRecordsTheRequestAndShowsStopping(t *testing.T) {
	c, st, _ := newTestServer(t)
	signed := signUpCustomer(t, c, "runner")
	fundAccount(t, st, signed.AccountID, 10_000_000)
	mustNode(t, st, "nod_1", signed.AccountID) // the node's owner doesn't matter to this test
	id := submitTaskForTest(t, c)
	node := "nod_1"
	require.NoError(t, st.TransitionTask(context.Background(), id, []store.TaskState{store.TaskQueued}, store.TaskRunning, store.TaskUpdate{NodeID: &node}))

	resp := c.do(http.MethodPost, "/api/portal/customer/tasks/"+id+"/cancel", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decodeBody[taskView](t, resp)
	require.Equal(t, "running", got.State, "it isn't finished until the node reports back")
	require.True(t, got.CancelRequested, "the UI shows 'Stopping'")

	again := decodeBody[taskView](t, c.do(http.MethodGet, "/api/portal/customer/tasks/"+id, nil))
	require.True(t, again.CancelRequested, "and keeps showing it on a reload")
}

func TestStopSomeoneElsesOrAFinishedTaskIsRefused(t *testing.T) {
	c, st, _ := newTestServer(t)
	owner := signUpCustomer(t, c, "owner")
	fundAccount(t, st, owner.AccountID, 10_000_000)
	id := submitTaskForTest(t, c)

	other := newTestClientFor(t, c)
	signUpCustomer(t, other, "stranger")
	resp := other.do(http.MethodPost, "/api/portal/customer/tasks/"+id+"/cancel", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "someone else's task looks like it doesn't exist")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodPost, "/api/portal/customer/tasks/tsk_nope/cancel", nil).StatusCode)

	require.NoError(t, st.TransitionTask(context.Background(), id, []store.TaskState{store.TaskQueued}, store.TaskSucceeded, store.TaskUpdate{}))
	resp = c.do(http.MethodPost, "/api/portal/customer/tasks/"+id+"/cancel", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, "a finished task can't be stopped")
}

// newTestClientFor returns a second client (its own cookie jar) against the same server.
func newTestClientFor(t *testing.T, c *testClient) *testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &testClient{t: t, base: c.base, hc: &http.Client{Jar: jar}}
}

// --- Gateway "Test connection" (phase 8, task 8.13) ---

// scriptedProber answers every probe with a fixed result.
type scriptedProber struct{ result quic.GatewayProbe }

func (p scriptedProber) ProbeGateway(_ context.Context, _ string, services []string) quic.GatewayProbe {
	out := p.result
	out.Services = nil
	for i, name := range services {
		if i < len(p.result.Services) {
			s := p.result.Services[i]
			s.Name = name
			out.Services = append(out.Services, s)
		}
	}
	return out
}

func newTestServerWithProber(t *testing.T, p GatewayProber) (*testClient, store.Store) {
	t.Helper()
	st := testStore(t)
	bus := events.NewBus()
	srv := &Server{
		Store: st, Customer: &api.CustomerServer{Store: st, Rates: pricing.DefaultRates(), Bus: bus},
		Bus: bus, Prober: p, Log: discardLog(), Dev: true,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &testClient{t: t, base: ts.URL, hc: &http.Client{Jar: jar}}, st
}

func createGatewayForTest(t *testing.T, c *testClient, label string, services ...gatewayServiceView) string {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/portal/customer/gateways", createGatewayRequest{Label: label, Services: services})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return decodeBody[createGatewayResponse](t, resp).ID
}

func TestGatewayTestReportsEachState(t *testing.T) {
	two := []gatewayServiceView{{Name: "db", Port: 5432}, {Name: "files", Port: 8000}}
	answered := func(ok bool, msg string) quic.ServiceProbe {
		return quic.ServiceProbe{OK: ok, Answered: true, Error: msg, Ms: 3}
	}
	cases := []struct {
		name   string
		result quic.GatewayProbe
		want   string
	}{
		{"green", quic.GatewayProbe{Connected: true, RTTMs: 41, Verified: true, Services: []quic.ServiceProbe{answered(true, ""), answered(true, "")}}, "green"},
		{"yellow", quic.GatewayProbe{Connected: true, RTTMs: 41, Verified: true, Services: []quic.ServiceProbe{answered(true, ""), answered(false, "connection refused")}}, "yellow"},
		{"red", quic.GatewayProbe{}, "red"},
		{"grey", quic.GatewayProbe{Connected: true, RTTMs: 41, Verified: false, Services: []quic.ServiceProbe{{Error: "may need updating"}, {Error: "may need updating"}}}, "grey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestServerWithProber(t, scriptedProber{result: tc.result})
			signUpCustomer(t, c, "tester")
			id := createGatewayForTest(t, c, "gw", two...)

			resp := c.do(http.MethodPost, "/api/portal/customer/gateways/"+id+"/test", nil)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			got := decodeBody[gatewayTestResponse](t, resp)
			require.Equal(t, tc.want, got.Status)
			require.Equal(t, tc.result.Connected, got.Connected)
			require.Len(t, got.Services, 2, "one row per registered service, with its port")
			require.Equal(t, "db", got.Services[0].Name)
			require.Equal(t, 5432, got.Services[0].Port)
			require.NotZero(t, got.TestedAtMS)
		})
	}
}

func TestGatewayTestOfSomeoneElsesGatewayIsNotFound(t *testing.T) {
	c, _ := newTestServerWithProber(t, scriptedProber{result: quic.GatewayProbe{Connected: true, Verified: true}})
	signUpCustomer(t, c, "owner")
	id := createGatewayForTest(t, c, "mine", gatewayServiceView{Name: "db", Port: 5432})

	other := newTestClientFor(t, c)
	signUpCustomer(t, other, "stranger")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodPost, "/api/portal/customer/gateways/"+id+"/test", nil).StatusCode)
	require.Equal(t, http.StatusNotFound, other.do(http.MethodPost, "/api/portal/customer/gateways/gw_nope/test", nil).StatusCode)
}

func TestGatewayTestWithoutAProberIs503(t *testing.T) {
	c, _ := newTestServerWithProber(t, nil)
	signUpCustomer(t, c, "owner")
	id := createGatewayForTest(t, c, "mine", gatewayServiceView{Name: "db", Port: 5432})
	require.Equal(t, http.StatusServiceUnavailable, c.do(http.MethodPost, "/api/portal/customer/gateways/"+id+"/test", nil).StatusCode)
}

// Gateway traffic (task 8.14): totals on the list, a series and busiest tasks on the
// detail, per-service usage on the task, and none of it visible to another account.
func TestGatewayAndTaskTrafficAreReportedAndScoped(t *testing.T) {
	c, st, _ := newTestServer(t)
	me := signUpCustomer(t, c, "tara")
	fundAccount(t, st, me.AccountID, 10_000_000)
	gw := createGatewayForTest(t, c, "lab", gatewayServiceView{Name: "db", Port: 5432})
	taskID := submitTaskForTest(t, c)

	ctx := context.Background()
	require.NoError(t, st.RecordGatewayTraffic(ctx, store.GatewayTrafficReport{GatewayID: gw, TaskID: taskID, Service: "db", BytesToLocal: 100, BytesToTask: 4000, At: time.Now(), RecordTask: true}))
	require.NoError(t, st.RecordGatewayTraffic(ctx, store.GatewayTrafficReport{GatewayID: gw, TaskID: taskID, Service: "db", BytesToLocal: 20, BytesToTask: 500, Final: true, At: time.Now(), RecordTask: true}))

	list := decodeBody[[]gatewayView](t, c.do(http.MethodGet, "/api/portal/customer/gateways", nil))
	require.Len(t, list, 1)
	require.EqualValues(t, 120, list[0].Traffic.ReceivedFromTasksBytes)
	require.EqualValues(t, 4500, list[0].Traffic.SentToTasksBytes)
	require.EqualValues(t, 1, list[0].Traffic.Connections)

	detail := decodeBody[gatewayTrafficResponse](t, c.do(http.MethodGet, "/api/portal/customer/gateways/"+gw+"/traffic?range=7d", nil))
	require.Equal(t, "hour", detail.Step)
	require.EqualValues(t, 4500, detail.Totals.SentToTasksBytes)
	var seriesSum int64
	for _, p := range detail.Series {
		seriesSum += p.SentToTasksBytes
	}
	require.EqualValues(t, 4500, seriesSum)
	require.Len(t, detail.Busiest, 1)
	require.Equal(t, taskID, detail.Busiest[0].TaskID)
	require.Equal(t, "day", decodeBody[gatewayTrafficResponse](t, c.do(http.MethodGet, "/api/portal/customer/gateways/"+gw+"/traffic?range=30d", nil)).Step)
	require.Equal(t, http.StatusBadRequest, c.do(http.MethodGet, "/api/portal/customer/gateways/"+gw+"/traffic?range=1y", nil).StatusCode)

	tt := decodeBody[taskTrafficResponse](t, c.do(http.MethodGet, "/api/portal/customer/tasks/"+taskID+"/traffic", nil))
	require.Len(t, tt.Rows, 1)
	require.Equal(t, "db", tt.Rows[0].Service)
	require.EqualValues(t, 120, tt.Totals.ReceivedFromTasksBytes)
	var seriesSent int64
	for _, p := range tt.Series {
		seriesSent += p.SentToTasksBytes
	}
	require.EqualValues(t, 4500, seriesSent, "the task's gateway-side series adds up to its total")

	// Someone else sees none of it.
	other := &testClient{t: t, base: c.base, hc: mustJarClient(t)}
	signUpCustomer(t, other, "olga")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodGet, "/api/portal/customer/gateways/"+gw+"/traffic", nil).StatusCode)
	require.Equal(t, http.StatusNotFound, other.do(http.MethodGet, "/api/portal/customer/tasks/"+taskID+"/traffic", nil).StatusCode)
	require.Empty(t, decodeBody[[]gatewayView](t, other.do(http.MethodGet, "/api/portal/customer/gateways", nil)))
}

// Machine usage (task 8.15): not supported until the agent reports, then a series with the
// biggest tasks named and the rest folded into "others"; task rows carry their totals; all of
// it scoped to the machine's own provider.
func TestMachineUsageSeriesTaskTotalsAndScoping(t *testing.T) {
	c, st, _ := newTestServer(t)
	me := signUpProvider(t, c, "uma")
	mustNode(t, st, "nod_u", me.AccountID)
	customer := signUpCustomer(t, &testClient{t: t, base: c.base, hc: mustJarClient(t)}, "cy")
	ctx := context.Background()

	// An agent that never reported usage.
	before := decodeBody[nodeUsageResponse](t, c.do(http.MethodGet, "/api/portal/provider/nodes/nod_u/usage", nil))
	require.False(t, before.Supported)
	require.Empty(t, before.Series)

	// More tasks than get their own band.
	tunnel := func(i int, n int64) int64 { // only the biggest CPU consumer and the smallest move data
		if i == usageTopTasks+1 || i == 0 {
			return n
		}
		return 0
	}
	var sample []store.TaskUsageSample
	for i := 0; i < usageTopTasks+2; i++ {
		id := "tsk_u" + string(rune('a'+i))
		require.NoError(t, st.CreateTask(ctx, store.Task{
			ID: id, AccountID: customer.AccountID, State: store.TaskQueued, Image: "alpine@sha256:a",
			Limits:       store.Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 128, WallTimeoutS: 60},
			Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"}, Delivery: store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
		}))
		_, err := st.(*store.PostgresStore).Pool().Exec(ctx, `UPDATE tasks SET node_id = 'nod_u' WHERE id = $1`, id)
		require.NoError(t, err)
		sample = append(sample, store.TaskUsageSample{TaskID: id, CPUCores: float64(i+1) / 10, MemoryBytes: int64(i+1) << 20, TunnelToGateway: tunnel(i, 100), TunnelToTask: tunnel(i, 200)})
	}
	require.NoError(t, st.RecordNodeUsage(ctx, store.NodeUsageSample{
		NodeID: "nod_u", At: time.Now().Add(-10 * time.Minute), IntervalMS: 15000, HostCPUBusy: 0.5, HostCPUCount: 4,
		HostMemTotal: 8 << 30, HostMemUsed: 2 << 30, DiskTotal: 100 << 30, DiskUsed: 30 << 30, Tasks: sample,
	}))

	got := decodeBody[nodeUsageResponse](t, c.do(http.MethodGet, "/api/portal/provider/nodes/nod_u/usage?range=24h", nil))
	require.True(t, got.Supported)
	require.Equal(t, "5m", got.Step)
	require.NotNil(t, got.Latest)
	require.Len(t, got.Latest.Tasks, usageTopTasks+2)
	require.Len(t, got.Tasks, usageTopTasks+1, "the biggest by CPU get a band, and so does the one task that is heavy on network but tiny on CPU")
	require.Contains(t, got.Tasks, "tsk_ua", "a network-heavy task must not be hidden in others on the network chart")
	require.Equal(t, "tsk_u"+string(rune('a'+usageTopTasks+1)), got.Tasks[0], "biggest consumer first")
	require.Len(t, got.Series, 1)
	pt := got.Series[0]
	require.InDelta(t, 0.5, pt.HostCPUBusy, 1e-9)
	var shares float64
	for _, sh := range pt.PerTask {
		shares += sh.CPUCores
	}
	require.InDelta(t, pt.TasksCPU, shares, 1e-9, "the bands (including others) add up to the machine's task total")
	require.Contains(t, pt.PerTask, usageOthersKey)
	require.EqualValues(t, 200, pt.PerTask["tsk_ua"].TunnelInBytes)
	require.EqualValues(t, 2*100, pt.TunnelOutBytes, "machine total = the two tasks that moved data")
	require.EqualValues(t, 2*200, pt.TunnelInBytes)
	require.Equal(t, "hour", decodeBody[nodeUsageResponse](t, c.do(http.MethodGet, "/api/portal/provider/nodes/nod_u/usage?range=7d", nil)).Step)
	require.Equal(t, http.StatusBadRequest, c.do(http.MethodGet, "/api/portal/provider/nodes/nod_u/usage?range=1y", nil).StatusCode)

	// The machine's task list carries each task's totals; a task never reported has none.
	tasks := decodeBody[[]nodeTaskView](t, c.do(http.MethodGet, "/api/portal/provider/nodes/nod_u/tasks", nil))
	require.Len(t, tasks, usageTopTasks+2)
	for _, tv := range tasks {
		require.NotNil(t, tv.Usage, tv.ID)
	}

	// Another provider can't see it.
	other := &testClient{t: t, base: c.base, hc: mustJarClient(t)}
	signUpProvider(t, other, "otto")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodGet, "/api/portal/provider/nodes/nod_u/usage", nil).StatusCode)
}

// The customer's own task chart data: readings against the limits asked for, scoped to the owner.
func TestCustomerTaskUsageIsScopedAndCarriesLimits(t *testing.T) {
	c, st, _ := newTestServer(t)
	me := signUpCustomer(t, c, "tess")
	fundAccount(t, st, me.AccountID, 10_000_000)
	provider := signUpProvider(t, &testClient{t: t, base: c.base, hc: mustJarClient(t)}, "pru")
	mustNode(t, st, "nod_t", provider.AccountID)
	taskID := submitTaskForTest(t, c)
	ctx := context.Background()
	_, err := st.(*store.PostgresStore).Pool().Exec(ctx, `UPDATE tasks SET node_id = 'nod_t' WHERE id = $1`, taskID)
	require.NoError(t, err)

	none := decodeBody[taskUsageResponse](t, c.do(http.MethodGet, "/api/portal/customer/tasks/"+taskID+"/usage", nil))
	require.False(t, none.Supported)
	require.Empty(t, none.Points)
	require.Equal(t, 1.0, none.Limits.Cores)
	require.Equal(t, 256, none.Limits.MemoryMB)

	now := time.Now()
	for i, tun := range []int64{0, 1000, 3000} {
		require.NoError(t, st.RecordNodeUsage(ctx, store.NodeUsageSample{
			NodeID: "nod_t", At: now.Add(time.Duration(i) * 15 * time.Second), IntervalMS: 15000, HostCPUCount: 2,
			Tasks: []store.TaskUsageSample{{TaskID: taskID, CPUCores: 0.5, MemoryBytes: 100 << 20, TunnelToGateway: tun}},
		}))
	}
	got := decodeBody[taskUsageResponse](t, c.do(http.MethodGet, "/api/portal/customer/tasks/"+taskID+"/usage", nil))
	require.True(t, got.Supported)
	require.Len(t, got.Points, 3)
	require.EqualValues(t, 1000, got.Points[1].TunnelOutBytes)
	require.EqualValues(t, 2000, got.Points[2].TunnelOutBytes)
	require.EqualValues(t, 3000, got.Summary.TunnelToGateway)
	require.Less(t, got.Points[0].AtMS, got.Points[2].AtMS)

	other := &testClient{t: t, base: c.base, hc: mustJarClient(t)}
	signUpCustomer(t, other, "nosy")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodGet, "/api/portal/customer/tasks/"+taskID+"/usage", nil).StatusCode)
}
