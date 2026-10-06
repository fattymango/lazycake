# Phase 7 (Provider/Customer Portals) — Progress

Same convention as `docs/00-core-platform/PROGRESS.md`: one dated entry per
task, written honestly about what's actually done versus deferred.

- **7.6-7.9 (frontend, ahead of 7A) — Scaffolding, shared components, auth
  pages, both portals' pages.** Explicit project-owner direction: build
  Phase 7B (frontend) only, before Phase 7A (backend, tasks 7.1-7.5) exists,
  despite `IMPLEMENTATION.md`'s own stated order ("7A must land before 7B
  can call real endpoints"). Nothing here has been run against a live
  backend — the user will build/wire 7A and test locally.

  `web/` is a Vite + React + TypeScript + Tailwind project with three HTML
  entry points (`index.html` the two-link chooser, `customer.html`,
  `provider.html`), sharing `src/shared/`: `api.ts` (fetch wrapper, sends
  the session cookie via `credentials: "include"`, throws `ApiError` on
  non-2xx), `auth.tsx` (`useCustomerAuth`/`useProviderAuth` + `RequireAuth`
  route guard), `hooks/useSSE.ts`, and components (`NavShell`, `StatTile`,
  `DataTable`, `StateBadge`/`ConnectedBadge` — reusing the exact
  `state-*` → color mapping from
  `internal/coordinator/dashboard/index.html` per `PLAN.md` §6 — `Panel`,
  `Form` inputs, `LogViewer`, `AuthForm`, loading/error/empty states).

  Customer pages: `Dashboard`, `SubmitTask`, `TaskHistory`, `TaskDetail`
  (live logs + live state over SSE), `Gateways`, `Billing` (ledger + dev-only
  add-funds). Provider pages: `Dashboard`, `AddMachine` (mints an install
  token, shows the copy-pasteable install command, warns it's shown once),
  `MachineDetail` (capacity/trust + recent tasks), `Earnings`.

  `internal/coordinator/webassets` go:embeds `web/`'s build output
  (`dist/`, checked in as an empty placeholder — `dist/.gitkeep` — so
  `go build ./...` keeps working with no frontend build present).
  `deploy/Dockerfile` gained a `node:22-alpine` `web-build` stage that the
  `build` stage copies `dist/` from before compiling, so the single-binary
  deploy story is unchanged. Verified: `go build ./...` and
  `go vet ./internal/coordinator/webassets/...` both pass with the repo as
  it stands (no `portalapi` package yet, so nothing imports it from
  `cmd/coordinator`).

  **Not done, deliberately, because 7A doesn't exist yet at the time of
  this entry (see the 7.1-7.5 entry below, added the same session once 7A
  did land):**
  - No task 7.10 backend changes: `internal/coordinator/dashboard` was left
    exactly as-is (still mounted at `/`, not `/ops`), and nothing wires
    `webassets` into `cmd/coordinator/run.go`. Moving the existing
    dashboard and serving the built frontend is real routing surgery
    that belongs with its own task, not something to bolt on speculatively.
  - Every page's `Done when` in `IMPLEMENTATION.md` requires a real running
    coordinator exercised from an actual browser; none of that has been
    done - only the backend's own HTTP-level tests (see below) exist so
    far. The API contract the frontend calls (`web/src/shared/types.ts`,
    `api.ts`) was the frontend's best-effort reading of `PLAN.md`'s
    Appendix and `store/types.go` at the time it was built, not a contract
    7A had confirmed yet - now that 7A exists, diff the frontend's types
    against `internal/coordinator/portalapi`'s actual wire shapes
    (`customer_handlers.go`/`provider_handlers.go`/`events_handler.go`)
    before trusting them; a few are already known to differ (see below).

  **Update, same session, after 7A landed:** `npm run build`, `tsc
  --noEmit`, and `npx eslint` were all actually run successfully (network
  access to npm's registry was available after all) - the "no network
  access" note above was wrong. `internal/coordinator/webassets/dist/` was
  verified to embed real built output correctly, then reset back to its
  placeholder before committing (build artifacts don't belong in git).

- **7.1-7.5 (backend) — Schema, session auth, store/event account
  scoping, customer and provider portal endpoints.** All five tasks done
  and wired into `cmd/coordinator/run.go`; verified with real Postgres
  (migrations applied, integration tests passing), not just compiled.

  **7.1** — `migrations/012_portal_credentials.sql`,
  `migrations/013_sessions.sql` exactly as specified.
  `store.PortalCredential`/`store.Session` structs plus a `PortalAuth`
  sub-interface (`CreatePortalCredential`, `GetPortalCredentialByUsername`,
  `CreateSession`, `GetSession`, `RevokeSession`) folded into `Store`,
  implemented in `postgres_portal.go`. `GetSession` filters out revoked
  and expired rows in the query itself (`WHERE revoked_at IS NULL AND
  expires_at > now()`), so no caller has to remember to check those fields
  separately - same pattern `Authenticate` already uses for bearer tokens.

  **7.2** — `internal/coordinator/portalapi`: `session.go` (cookie
  issue/verify, `requireSession` middleware, context helpers),
  `auth_handlers.go` (signup/login/logout), `lockout.go` (`loginLimiter`:
  10 failures within 5 minutes locks a username out for 5 minutes,
  in-memory map as the plan itself calls for). One generic "invalid
  username or password" error covers a wrong password, an unknown
  username, *and* a credential registered for the other portal - none of
  the three is distinguishable from the response alone. Cookie is httpOnly,
  `SameSite=Lax`, `Secure` unless `cfg.Dev`, 7-day expiry, hashed before
  storage/lookup exactly like `api_tokens`.

  **7.3** — `Store.ListNodesByAccount`/`Store.ListTasksByAccount` added
  next to the existing fleet-wide queries. `events.Event` gained an
  `AccountID` field (`json:"-"` - it's a filter key for `Bus`, never part
  of the wire format either the `/ops` dashboard or a portal's own stream
  sees), and `events.Bus` gained `SubscribeAccount(accountID)` alongside
  the existing unfiltered `Subscribe()`, implemented by storing each
  subscriber's filter string (`""` = fleet-wide) instead of a bare set.
  Every existing `Publish` call site across `api` and `scheduler` was
  updated to set it - task events get the task's own `AccountID` (already
  in scope at every one of those call sites, or one `GetTask`/`GetNode`
  call already being made nearby for an unrelated reason, reordered to
  happen first rather than duplicated), node events get the node's owner
  (returned from `handleRegister`, threaded through `handleMessage` for
  `handleCapacity`). This is a real, if judgment-call, design point worth
  naming: a task_state event carries the *customer's* account, a
  node_connected/capacity event carries the *provider's* - matching
  exactly what each portal's own dashboard needs (PLAN.md §3: customer
  cares about task state, provider cares about machine connectivity), not
  a single ambiguous "whose event is this" field.

  **7.4** — `api.CustomerServer.SubmitTask` (the gRPC RPC) was refactored,
  not duplicated: its validation/creation body now lives in a new exported
  `SubmitTaskForAccount(ctx, accountID, SubmitTaskParams) (store.Task,
  error)`, transport-agnostic (plain Go types, not proto), returning the
  same `*status.Error`s as before so both the gRPC RPC and portalapi's
  `POST /api/portal/customer/tasks` get identical digest-pin, gateway-
  ownership, and balance-affordability behavior from one place -
  `writeStoreOrRPCError` maps the grpc `codes.*` back to HTTP statuses.
  All of `customer_server_test.go`'s existing tests (digest pin, too many
  gateways, unknown/other-account gateway) still pass unchanged against
  the refactored code, proving the behavior didn't shift under it.
  `customer_handlers.go` implements `GET /me`, `POST/GET /tasks`,
  `GET /tasks/{id}`, `GET /tasks/{id}/logs` (SSE, same poll-`ListLogs`-
  every-500ms shape `StreamLogs` already uses), `GET/POST /gateways`
  (`CreateGateway` reimplemented directly against the store rather than
  through the RPC-sharing pattern above, since it has no balance/ownership
  checks worth centralizing the same way), `GET /ledger`,
  `POST /balance/add` (dev-only, clearly commented as such - see
  OPEN_QUESTIONS.md on whether it should be gated behind `cfg.Dev` at the
  route level).

  **7.5** — `provider_handlers.go`: `GET /me` (lifetime earnings = sum of
  `kind='credit'` ledger rows), `GET /nodes`, `GET /nodes/{id}`,
  `POST /nodes/install-token` (mints a real `TokenAgent`-kind token via
  `Store.CreateToken`, shown once, plus a real runnable `podman run`
  command built from this repo's own agent image and env vars - not a
  placeholder string), `GET /ledger`. Also added
  `GET /nodes/{id}/tasks`, not in the plan's Appendix table (see
  OPEN_QUESTIONS.md's "invented" note from the frontend build) - machine
  detail needs `ListTasksByNode`, and folding a long task history into
  every node-detail response seemed worse than its own route.
  `cmd/coordinator/run.go` wires `portalapi.Server` into the existing
  `httpMux` alongside (not replacing) the operator dashboard, under
  `/api/portal/` - `config.Config` gained `PublicGRPCAddr`
  (`LAZYCAKE_PUBLIC_GRPC_ADDR`, defaults to `GRPCAddr`) purely so the
  install command can name a real dial address, since a bind address like
  `:7443` is frequently not what an agent behind NAT should actually dial.

  **Verified for real, not just compiled:** a local `postgresql-16`
  server was started in this sandbox specifically to run these tests
  against real Postgres rather than only against `go build`/`go vet` -
  migrations 001-013 applied cleanly (`goose ... up`), then:
  `go test -tags=integration ./internal/coordinator/store/...` (new
  `postgres_portal_test.go`: credential/session lifecycle, session expiry
  and revocation, account-scoped node/task listing),
  `go test ./internal/coordinator/events/...` (new `events_test.go`:
  `Subscribe` vs `SubscribeAccount` filtering, a nil-`AccountID` event
  never leaking into a filtered subscriber, `Publish` never blocking on a
  full/slow subscriber, nil-`Bus` no-op), `go test
  ./internal/coordinator/api/...` (existing suite, green after the
  `SubmitTask` refactor and the `fakeStore` test double's five new
  methods), and a new `go test -tags=integration
  ./internal/coordinator/portalapi/...` (18 tests: full signup/login/
  logout/lockout flow through a real cookie-jar HTTP client exactly like a
  browser, cross-role session rejection, task/gateway/node cross-account
  isolation, install-token round-tripping through real `Authenticate`,
  lifetime-earnings computed from a real `SettleTask` call, and
  account-scoped SSE actually filtering out another account's event over
  a real HTTP connection). `go build ./...`, `go build -tags=integration
  ./...`, and `go vet ./...` all pass; `gofmt -l` is clean.

  **One real caveat surfaced by running the full suite, not just this
  package's own tests:** `go test -tags=integration ./...` (matching
  `make test-integration` verbatim) intermittently fails elsewhere in the
  suite - not in this package - when Go's default parallel-package test
  runner lets `store`'s and `portalapi`'s integration tests run
  concurrently against the *same* real database: each package's own
  `testStore` helper does a blind `TRUNCATE ... CASCADE` per test, so one
  package's truncate can wipe rows another package's in-flight test still
  needs. `go test -tags=integration -p 1 ./...` (sequential packages)
  passes reliably; this is a pre-existing characteristic of the `store`
  package's own single-shared-database test convention, made newly
  visible by `portalapi` being the second integration-tagged package to
  use it, not a bug in either package. Not fixed here - it would mean
  redesigning test isolation (per-test transactions/schemas) across both
  packages, a bigger call than this task's scope - but worth flagging
  loudly rather than leaving for the next person to rediscover by
  surprise. `TestBenchStability` and every podman-socket-dependent test
  (`internal/agent/exec`, `internal/agent/lease`, `internal/agent/
  reconcile`, `internal/agent/runtime`, `internal/e2e`) were already
  failing before this session's changes, for the pre-existing, separately
  documented reasons in `docs/00-core-platform/OPEN_QUESTIONS.md` (no
  working cgroup v2 delegation / bench variance on this VM) - unrelated to
  Phase 7 and left exactly as found.

**Phase 7A (backend) complete** - all 5 tasks done and verified against
real Postgres. Phase 7B (frontend) tasks 7.6-7.9 are code-complete and now
have a real backend to be pointed at and exercised in an actual browser,
which has not been done yet; task 7.10 (retire landing page, relocate
`/ops`, serve the built frontend from the coordinator binary) is not
started.

- **Live verification pass (2026-09-25, commit `03a9c0c`) - the two
  halves actually run together, on real infrastructure, for the first
  time.** Checked out this branch on a VM with a working rootless podman
  engine (the same one `docs/00-core-platform/PROGRESS.md`'s VM-side
  session used) and ran the whole thing for real rather than reading the
  code: applied migrations 012/013 against live Postgres, ran
  `go test -tags integration -p 1` across `store`/`portalapi`/`events`
  (all pass), rebuilt the coordinator image (frontend `npm run build`
  runs inside `deploy/Dockerfile`'s new node stage and embeds cleanly via
  `internal/coordinator/webassets`), and ran `npm run dev` against the
  live coordinator through Vite's `/api/portal` proxy.

  Exercised the real customer flow through the new HTTP API specifically
  (not the pre-existing gRPC path): signup → login → dev-mode balance
  add → `POST .../tasks` → watched `queued → succeeded`, `exit_code: 0`,
  on a real connected agent. Exercised the real provider flow: signup →
  minted an install token → started a **real agent process** with it →
  it registered and showed up in `GET .../nodes`, correctly scoped to
  that account and no other.

  **Found and fixed one real bug this surfaced, not visible from reading
  the code:** `installCommand`'s `LAZYCAKE_OFFER_CORES=<cores>` etc. were
  literal angle-bracket placeholder text, not real values - looks fine
  printed in JSON, but bash parses an unquoted `<` as redirection, so
  copy-pasting the command as shown into a real shell fails immediately
  (`bash: cores: No such file or directory`) - caught only because the
  actual "copy this into a real terminal" step was actually attempted,
  not just verified by test assertions checking for a substring. Fixed to
  use real default values (matching `docker-compose.yml`'s own agent1
  example: 2 cores, 2048MB, 8192MB). While in there, also flagged (as a
  same-line shell comment, so it stays copy-paste-safe) the pre-existing,
  separately-known rootful-vs-rootless `podman.sock` path assumption -
  not a new bug, but the same class of "looks right, isn't" gap, worth
  fixing in the same pass. Re-verified after the fix: minted a fresh
  token, ran the corrected command with the rootless substitution applied
  (`podman build --target agent -t lazycake-agent .` first, per the
  command's own doc comment), and confirmed the agent registered.

  Not yet done: the actual React UI has not been visually inspected in a
  real browser by this session (only served/compiled/data-verified) -
  the project owner has been testing it directly in their own browser in
  parallel, which is in fact how the `<cores>` bug was first noticed (a
  copy-paste from the live UI failing in a real terminal).

- Public images + live gateway test, 2026-10-06 - published
  `docker.io/fattymango/lazycake-agent` and a new `cmd/lcbench` benchmark
  workload (`docker.io/fattymango/lcbench`). Ran a real gateway on a second
  machine (the Ubuntu VM, bridged onto the LAN) registered through the
  customer portal; a task on this machine's agent fetched a file from it
  through the droplet's relay, with the gateway logging
  `forwarded connection closed` and byte counts.

  Found while doing it, all fixed in this pass:
  - The install command pulled a bare `lazycake-agent`, which Docker Hub
    denies. It now uses the full public image name.
  - The install command's agent still failed *every* task: it resolved
    `lcinit` inside its own container but the engine resolves bind sources
    on the host (`mkdir /usr/local/bin/lcinit: permission denied`). New
    `LAZYCAKE_LCINIT_HOST_DIR`: the command mounts one directory at the same
    path on both sides and the agent stages its bundled `lcinit` there.
  - `conn.Runner.Send` read `sendCh` without the mutex while `runOnce`
    wrote it on every reconnect - a real data race (caught by `-race` in
    `TestSelfFence`). Now guarded.
  - `lcbench`'s own CPU readout spiked past the cap (a Ticker delivers a
    stale tick right after a late one when the process is throttled, giving
    a near-zero sampling interval). Now a plain sleep, with the counter and
    clock read together.

  Validated: `go test -p 1 -race -tags=integration ./...` all green except
  `internal/agent/bench`'s `TestBenchStability`, which needs <5% spread and
  measures ~6.6% on this laptop (CPU governor `powersave`; the code is
  identical to master, so this is the machine, not a regression). Note
  `-p 1` is required: the store, billing and portalapi tests truncate the
  same Postgres tables and interfere when run in parallel packages.

