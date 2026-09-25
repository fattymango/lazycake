# LazyCake Phase 7 — Provider and Customer Portals: Implementation Guide

Read `PLAN.md` in this directory first for the design rationale; this
document is the execution order, in the same style as
`docs/00-core-platform/IMPLEMENTATION.md`.

---

## 0. How to use this document

Same rules as `docs/00-core-platform/IMPLEMENTATION.md` §0: one task at a
time, run its verify command before moving on, commit after every task
with `7.<task>: <short description>`, create
`docs/01-dashboard-portals/PROGRESS.md` with your first commit of this
phase and keep it updated (one line per task, date, commit hash, exactly
like the root one), and log any judgment call in
`docs/01-dashboard-portals/OPEN_QUESTIONS.md` rather than silently
deciding or silently skipping. If a task can't be verified in your
environment, say so and mark it `BLOCKED: <reason>` — don't fake a pass.

This phase is organized as two sub-phases: **7A (backend)** must land
before **7B (frontend)** can call real endpoints, but 7B's component
library and static pages can be scaffolded against mocked data in
parallel if that's useful — your call, note it in
`docs/01-dashboard-portals/PROGRESS.md` if you do.

---

## 1. Phase 7A — Backend

### Task 7.1 — `sessions` table and provider token kind

**Goal:** the two schema changes `PLAN.md` §8.1–8.2 calls for.

**Files:** `migrations/012_sessions.sql`, `migrations/013_provider_token_kind.sql`.

```sql
-- 012_sessions.sql
-- +goose Up
CREATE TABLE sessions (
  id_hash     BYTEA PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  kind        TEXT NOT NULL CHECK (kind IN ('customer','provider')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL,
  revoked_at  TIMESTAMPTZ
);
CREATE INDEX sessions_account_id_idx ON sessions(account_id);

-- +goose Down
DROP TABLE sessions;
```

```sql
-- 013_provider_token_kind.sql
-- +goose Up
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_kind_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_kind_check
  CHECK (kind IN ('customer','agent','gateway','provider'));

-- +goose Down
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_kind_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_kind_check
  CHECK (kind IN ('customer','agent','gateway'));
```

Add `store.TokenProvider` alongside the existing `TokenKind` constants
(`internal/coordinator/store/types.go`), and a `Session` struct mirroring
`APIToken`'s shape (`IDHash`, `AccountID`, `Kind`, `CreatedAt`,
`ExpiresAt`, `RevokedAt`).

**Done when:** `goose ... up` applies cleanly against a fresh database
and `goose ... down` twice unwinds both, in either order relative to each
other (013 only touches the CHECK constraint, so order between 012/013
doesn't matter, but verify anyway).

**Verify:**
```sh
go run github.com/pressly/goose/v3/cmd/goose@v3.28.0 -dir migrations postgres "$LAZYCAKE_DATABASE_URL" up
go run github.com/pressly/goose/v3/cmd/goose@v3.28.0 -dir migrations postgres "$LAZYCAKE_DATABASE_URL" down
go run github.com/pressly/goose/v3/cmd/goose@v3.28.0 -dir migrations postgres "$LAZYCAKE_DATABASE_URL" down
```

### Task 7.2 — Session store methods

**Goal:** `Store` gains a `Sessions` sub-interface, implemented against
`sessions`.

**Files:** `internal/coordinator/store/store.go` (new `Sessions`
interface, added to `Store`), new
`internal/coordinator/store/postgres_sessions.go`.

```go
type Sessions interface {
    // CreateSession inserts a new session row. idHash is the same
    // sha256-of-random-bytes pattern auth.Hash already produces for
    // tokens - the raw session ID is never persisted.
    CreateSession(ctx context.Context, s Session) error
    // GetSession looks up a non-revoked, non-expired session by its
    // hashed ID. Returns ErrNotFound otherwise (expired and revoked
    // are indistinguishable to the caller, same as an unknown token).
    GetSession(ctx context.Context, idHash []byte) (Session, error)
    RevokeSession(ctx context.Context, idHash []byte) error
}
```

**Done when:** a unit test creates a session, fetches it, revokes it, and
confirms the post-revoke fetch returns `ErrNotFound`; a second test
confirms an expired-but-not-revoked session also returns `ErrNotFound`
(use a fake clock or an `expires_at` in the past at insert time).

**Verify:** `go test ./internal/coordinator/store/...`

### Task 7.3 — `ListNodesByAccount` and `ListTasksByAccount`

**Goal:** the two scoped queries `PLAN.md` §8.3 calls for.

**Files:** `internal/coordinator/store/store.go` (add both method
signatures to `Nodes`/`Tasks`), `postgres_tasks.go`, and wherever
`ListNodes`/`ListRecentTasks` are implemented (find via `grep -n "func.*ListNodes\b" internal/coordinator/store/*.go`).

`ListTasksByAccount` takes the same `limit` shape as `ListRecentTasks`
(newest first); `ListNodesByAccount` takes no limit (a provider's own
fleet is never going to be page-worthy at this project's scale).

**Done when:** a unit/integration test seeds two accounts with nodes and
tasks each and confirms each account's query returns only its own rows,
in the right order.

**Verify:** `go test ./internal/coordinator/store/...`

### Task 7.4 — Per-account event scoping

**Goal:** `events.Event` carries `AccountID`; every existing `Publish`
call site sets it; a subscriber can filter to one account.

**Files:** `internal/coordinator/events/events.go` (add field; add an
`AccountID` param — or a filtering `Subscribe` variant — so a caller can
subscribe to one account's events without every subscriber doing its own
filtering), every existing `Bus.Publish(events.Event{...})` call site
(`grep -rn "Publish(events.Event{" internal/`).

Keep the existing unfiltered `Subscribe()` behavior for `/ops` (the
relocated dashboard, task 7.13) — add rather than replace.

**Done when:** a unit test publishes events for two different accounts
into the bus and confirms an account-scoped subscriber only receives its
own.

**Verify:** `go test ./internal/coordinator/events/...`

### Task 7.5 — `internal/coordinator/portalapi`: sessions and auth middleware

**Goal:** the package `PLAN.md` §5 describes, starting with the auth
seam everything else in 7A sits behind.

**Files:** new `internal/coordinator/portalapi/` package:
`session.go` (cookie issuance/verification, matching `auth.Hash`'s
pattern for the session ID itself), `middleware.go` (a handler wrapper
that resolves the cookie to a `store.Session`, 401s otherwise, and puts
the resolved account ID + kind on the request context), `server.go`
(mux wiring, mirroring `dashboard.Server`'s `Handler()` shape).

Cookie: httpOnly, `Secure` (skip the flag only when `cfg.Dev`, matching
how `LAZYCAKE_DEV` already relaxes other things), `SameSite=Lax`,
reasonable expiry (e.g. 7 days), session ID is 32 random bytes,
base64url-encoded in the cookie, sha256-hashed the same way before any
DB lookup or write - never store or log the raw value.

**Done when:** a test issues a session, builds a request with its
cookie, and confirms the middleware resolves the right account/kind; a
second test confirms a missing/garbage/expired cookie 401s.

**Verify:** `go test ./internal/coordinator/portalapi/...`

### Task 7.6 — Login and account-creation endpoints

**Goal:** `POST /api/portal/{customer,provider}/login` and
`POST /api/portal/{customer,provider}/signup`, plus
`POST /api/portal/logout`.

**Files:** `internal/coordinator/portalapi/auth_handlers.go`.

`login`: body `{"token": "..."}`, hash it, `Store.Authenticate`, reject
if the token's `Kind` doesn't match the portal (`customer` token on
`/customer/login`, `provider` token on `/provider/login`), otherwise
create a session and set the cookie, return `{"account_id": "..."}`.

`signup`: no body needed. Create a fresh `Account` (starting balance —
reuse whatever `seed.EnsureDemoAccount` uses for the demo account as the
default provisioning balance, or `0` for a provider since they earn
rather than spend; decide and note it in
`docs/01-dashboard-portals/OPEN_QUESTIONS.md` if it's not obvious which),
mint a token of the matching kind (`customer` or `provider`), create a
session, set the cookie, return `{"account_id": "...", "token": "..."}` —
the one and only time the raw token is returned.

`logout`: revoke the session, clear the cookie.

**Done when:** an integration test drives signup → gets a working
session cookie → login with the returned token also works → logout
invalidates the cookie (a subsequent authenticated call 401s).

**Verify:** `go test ./internal/coordinator/portalapi/...`

### Task 7.7 — Customer portal endpoints

**Goal:** the REST surface for `PLAN.md` §6.1's table.

**Files:** `internal/coordinator/portalapi/customer_handlers.go`.

Reuse `api.CustomerServer`'s logic rather than reimplementing it —
either by having `portalapi` call into a small shared helper package
both `api.CustomerServer` and `portalapi` depend on (extract
`SubmitTask`'s validation/pricing/hold logic into
`internal/coordinator/tasksubmit` or similar), or by having
`portalapi.CustomerHandlers` hold a `*api.CustomerServer` and call its
Go methods directly with a context carrying a constructed
`store.APIToken` — pick whichever is less invasive once you're looking
at the real code; note the choice and why in
`docs/01-dashboard-portals/OPEN_QUESTIONS.md` if it's not obvious.
Whichever you pick, **do not duplicate the validation rules** (digest-pin
check, gateway ownership, balance check) in a second place that can
silently drift from the gRPC path's.

Endpoints:
- `GET /api/portal/customer/me` → balance, available balance
- `POST /api/portal/customer/tasks` → wraps `SubmitTask`
- `GET /api/portal/customer/tasks` → `ListTasksByAccount` (task 7.3)
- `GET /api/portal/customer/tasks/{id}` → wraps `GetTask`, 404s if not
  this account's task (reuse the existing ownership check)
- `GET /api/portal/customer/tasks/{id}/logs` → SSE, not gRPC streaming;
  same polling-`ListLogs`-in-a-loop shape `StreamLogs` already uses
  server-side, just re-emitted as `data:` lines
- `GET /api/portal/customer/gateways`, `POST /api/portal/customer/gateways`
  → wrap `ListGateways`/`CreateGateway`
- `GET /api/portal/customer/ledger` → `LedgerEntriesForAccount`
- `POST /api/portal/customer/balance/add` → dev-mode `AdjustBalance`,
  clearly named and documented as not-a-real-payment-rail (see `PLAN.md`
  §3)

All behind the task 7.5 middleware, scoped to the session's own
`account_id` throughout — never trust a client-supplied account ID.

**Done when:** an integration test exercises every endpoint above
end-to-end against a real Postgres (task, gateway, ledger, balance), and
confirms a second account's session gets 404/empty results for the
first account's resources, not an error revealing they exist (matching
`GetTask`'s existing not-found-not-forbidden posture).

**Verify:** `go test ./internal/coordinator/portalapi/...`

### Task 7.8 — Provider portal endpoints

**Goal:** the REST surface for `PLAN.md` §6.2's table, including the
"add a machine" flow that closes the gap in `PLAN.md` §4.1.

**Files:** `internal/coordinator/portalapi/provider_handlers.go`.

Endpoints:
- `GET /api/portal/provider/me` → lifetime earnings summary (sum of
  `LedgerEntriesForAccount` where `kind = credit`)
- `GET /api/portal/provider/nodes` → `ListNodesByAccount` (task 7.3)
- `GET /api/portal/provider/nodes/{id}` → `GetNode` + `ListTasksByNode`,
  404 if the node isn't this account's
- `POST /api/portal/provider/nodes/install-token` → mints a fresh
  `agent`-kind token for this account (the actual gap-closer:
  today this only happens via `seed`/direct store access), returns it
  once with the same "won't be shown again" contract as
  `CreateGateway`'s `install_token`
- `GET /api/portal/provider/ledger` → `LedgerEntriesForAccount`

**Done when:** an integration test mints an install token via the
endpoint, confirms it authenticates a real `AgentService.Connect` call
(reuse `internal/e2e`'s existing agent-connect test setup rather than
hand-rolling a new one), and confirms node/ledger listing is scoped
per-account the same way task 7.7 verifies for the customer side.

**Verify:** `go test ./internal/coordinator/portalapi/... ./internal/e2e/...`

### Task 7.9 — CSRF protection

**Goal:** `PLAN.md` §10's `SameSite=Lax` + custom-header check.

**Files:** `internal/coordinator/portalapi/middleware.go`.

Every mutating endpoint (`POST`/`PUT`/`DELETE`) requires a
`X-LazyCake-Portal: 1` header (or similar), checked before touching the
session — a cross-site form/fetch without JS control over headers can't
set it, a same-origin `fetch` from the real frontend always does.
Reject with 403 if missing.

**Done when:** a test confirms a mutating request without the header is
rejected even with a valid session cookie attached, and confirms one
with the header succeeds.

**Verify:** `go test ./internal/coordinator/portalapi/...`

### Task 7.10 — Wire `portalapi` into `cmd/coordinator`

**Goal:** the new package actually serves traffic.

**Files:** `cmd/coordinator/run.go`.

Mount `portalapi.Server{...}.Handler()` under `/api/portal/` on the
existing `httpServer` mux (same `cfg.HTTPAddr` — see `PLAN.md` §5's "one
HTTP listener" point). Thread through whatever `portalapi.Server` needs
(`Store`, `Bus`, pricing `Rates`, session cookie config) the same way
`dashboard.Server` is already constructed just below it.

**Done when:** `podman compose -f deploy/docker-compose.yml up -d --build`
brings up a coordinator that answers `curl -i localhost:8080/api/portal/customer/login`
with something other than a connection error (a 400 for a missing body
is fine — the point is the route exists and is reachable).

**Verify:** manual `curl` smoke test as above, plus the full
`go test ./...` suite still green.

---

## 2. Phase 7B — Frontend

### Task 7.11 — Frontend scaffolding

**Goal:** `PLAN.md` §9.1's toolchain, producing embeddable static assets.

**Files:** new `web/` directory at repo root: `web/package.json`,
`web/vite.config.ts` (two entry points, `provider.html`/`customer.html`,
or two separate Vite apps under `web/provider/` and `web/customer/` if
that ends up cleaner once you're building it — your call), `web/tailwind.config.ts`,
shared component library under `web/shared/`.

Wire a `go:embed` in a new `internal/coordinator/webassets` package
pointing at the Vite build output directory, and a Makefile/script
target (`web/build.sh` or similar) that runs `npm ci && npm run build`
before `go build` — mirror this in `deploy/Dockerfile`'s `coordinator`
build stage (add a `node:22-alpine` stage, copy its output into the
existing `build` stage before `go build`).

**Done when:** `npm run build` inside `web/` produces static output, a
placeholder Go program embeds and serves it, and
`podman build --target coordinator -f deploy/Dockerfile .` succeeds with
the new Node build stage included.

**Verify:**
```sh
cd web && npm ci && npm run build
podman build --target coordinator -t lazycake-coordinator-test -f deploy/Dockerfile ..
```

### Task 7.12 — Shared component library

**Goal:** the component inventory from `PLAN.md` §9.2's last bullet,
built once against the design tokens (colors, spacing, type scale)
configured in `tailwind.config.ts`.

**Files:** `web/shared/components/`: `NavShell`, `StatTile`, `DataTable`,
`StateBadge` (reuse the existing state→color mapping from
`internal/coordinator/dashboard/index.html`'s `.state-*` CSS classes as
the source of truth for which state gets which color), `FormField`,
`Toast`, `CopyableToken`, `LogViewer`.

**Done when:** a Storybook-free but visually-checkable approach works —
either a small `/dev/components` route in each app during local dev, or
Storybook itself if you'd rather set it up (your call, note it in
`docs/01-dashboard-portals/OPEN_QUESTIONS.md` either way); every
component in the inventory renders with representative data and covers
loading/empty/error variants where applicable.

**Verify:** manual visual check (`npm run dev`), plus
`npm run typecheck` / `npm run lint` clean.

### Task 7.13 — Auth pages (both portals)

**Goal:** login + create-account screens wired to task 7.6's endpoints.

**Files:** `web/provider/pages/Login.tsx`, `web/customer/pages/Login.tsx`
(or shared with a `portal: "provider" | "customer"` prop if the two end
up identical apart from copy/endpoint prefix — likely, given `PLAN.md`
§4's flow is the same for both).

The "here is your token, save it" reveal after signup must make copying
trivial (the `CopyableToken` component from task 7.12) and must not
auto-navigate away before the user acknowledges it.

**Done when:** signing up, seeing the token once, and logging back in
with it works end-to-end against a running coordinator; refreshing the
page after login stays signed in (cookie persists); signing out and
hitting a protected route redirects to login.

**Verify:** manual run against `podman compose ... up`, plus component
tests for the form validation states.

### Task 7.14 — Customer portal pages

**Goal:** every row in `PLAN.md` §6.1's table, built.

**Files:** `web/customer/pages/{Overview,SubmitTask,TaskDetail,TaskHistory,Gateways,Billing}.tsx`.

`TaskDetail`'s log view subscribes to task 7.7's SSE endpoint and
auto-scrolls; `SubmitTask`'s form mirrors `lcctl submit`'s flags
(image, cpu/memory/disk/timeout limits, entrypoint/args, delivery mode,
gateway targets) with inline validation matching the server's own
rules (digest-pin format, wall_timeout_s required) so a bad submission
never round-trips to the server to get its first error.

**Done when:** every action in the table is possible without leaving the
browser: sign in, submit a real task against a running coordinator+agent,
watch it go queued → running → succeeded live, see it in history, create
a gateway, see the balance move after settlement.

**Verify:** manual end-to-end run against `podman compose -f deploy/docker-compose.yml up`
(same stack the chaos demo uses), submitting a real task through the UI
and watching it complete; component/unit tests for form validation and
data-table rendering.

### Task 7.15 — Provider portal pages

**Goal:** every row in `PLAN.md` §6.2's table, built.

**Files:** `web/provider/pages/{Overview,AddMachine,NodeDetail,Earnings}.tsx`.

`AddMachine` mints a token (task 7.8's endpoint) and shows the exact
`podman compose`/env-var incantation needed to point a real agent at
this coordinator with it — copy-pasteable, not just the bare token.

**Done when:** sign in, add a machine, get a token + install snippet,
actually start a real agent with it (`docker-compose.yml`'s
`LAZYCAKE_TOKEN`, or a bare `./bin/agent` run) and watch it show up
connected in the portal within one heartbeat interval; node detail shows
real task history and trust score for a node that's actually run
something; earnings reflects a real settled task's credit.

**Verify:** manual end-to-end run, same stack as task 7.14; component
tests for the add-machine flow's states (pending, token shown, machine
connected).

### Task 7.16 — Real-time updates

**Goal:** both portals reflect state changes live, not only on page
load/refresh — the same "live dashboard" property the current one has,
scoped per-account (task 7.4).

**Files:** `web/shared/hooks/usePortalEvents.ts`, wired into `Overview`,
`TaskDetail`, `NodeDetail` in both apps.

**Done when:** with a portal open, submitting a task from a second
browser tab (or `lcctl`) updates the first tab's task list/overview
without a manual refresh; a provider's node connecting/disconnecting
updates their overview live.

**Verify:** manual two-tab check.

### Task 7.17 — Responsive and accessibility pass

**Goal:** both portals usable at mobile width (the `dataviz`/
`artifact-design` house style this project's other surfaces follow — no
horizontal scroll, readable at a phone width) and with keyboard-only
navigation through every interactive element (forms, tables, nav).

**Files:** touch-up pass across `web/shared` and both apps' pages.

**Done when:** every page works at 375px width with no horizontal
scroll and no unreachable controls, and every interactive element is
reachable and operable via keyboard alone (tab order, visible focus
states, no click-only handlers on non-button elements).

**Verify:** manual check at a narrow viewport plus keyboard-only manual
pass; an automated `axe-core` check in CI is a reasonable addition here
if you want one, not required to call this task done.

### Task 7.18 — Retire the old landing page; relocate the ops dashboard

**Goal:** `PLAN.md` §7's move.

**Files:** `internal/coordinator/dashboard/dashboard.go` (mount its
`Handler()` under `/ops` instead of `/`), a small new landing handler at
`/` offering "Provider" / "Customer" links to `/portal/provider` and
`/portal/customer` (the frontend routes task 7.11's build serves),
`deploy/docker-compose.yml` and `README.md` updated to point at the new
paths, `deploy/chaos-demo.sh`'s "open the dashboard" comment updated to
`/ops`.

**Done when:** `podman compose -f deploy/docker-compose.yml up` serves a
working chooser at `/`, the ops view at `/ops` unchanged in behavior,
and both portals reachable and functional from the chooser — i.e. the
exact same manual verification `HANDOVER.md`'s original demo steps
described, now against three surfaces instead of one.

**Verify:** the full manual walkthrough — bring up the stack, submit a
task through the customer portal, add and connect a machine through the
provider portal, confirm `/ops` still shows the global view — plus
`go test ./...` and (if you set one up) the frontend's own test suite,
all green.

---

## 3. Appendix — endpoint summary

| Method | Path | Portal | Auth |
|---|---|---|---|
| POST | `/api/portal/customer/signup` | customer | none (creates session) |
| POST | `/api/portal/customer/login` | customer | none (creates session) |
| POST | `/api/portal/provider/signup` | provider | none (creates session) |
| POST | `/api/portal/provider/login` | provider | none (creates session) |
| POST | `/api/portal/logout` | either | session |
| GET | `/api/portal/customer/me` | customer | session |
| POST | `/api/portal/customer/tasks` | customer | session |
| GET | `/api/portal/customer/tasks` | customer | session |
| GET | `/api/portal/customer/tasks/{id}` | customer | session |
| GET | `/api/portal/customer/tasks/{id}/logs` | customer | session, SSE |
| GET/POST | `/api/portal/customer/gateways` | customer | session |
| GET | `/api/portal/customer/ledger` | customer | session |
| POST | `/api/portal/customer/balance/add` | customer | session |
| GET | `/api/portal/provider/me` | provider | session |
| GET | `/api/portal/provider/nodes` | provider | session |
| GET | `/api/portal/provider/nodes/{id}` | provider | session |
| POST | `/api/portal/provider/nodes/install-token` | provider | session |
| GET | `/api/portal/provider/ledger` | provider | session |
| GET | `/api/portal/{customer,provider}/events` | either | session, SSE |
