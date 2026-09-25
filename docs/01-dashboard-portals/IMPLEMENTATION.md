# LazyCake Phase 7 — Provider and Customer Portals: Implementation Guide

Read `PLAN.md` in this directory first. Same rules as
`docs/00-core-platform/IMPLEMENTATION.md` §0: one task at a time, verify
before moving on, commit as `7.<task>: <description>`, create
`docs/01-dashboard-portals/PROGRESS.md` with your first commit and keep
it updated, log real judgment calls in this directory's
`OPEN_QUESTIONS.md`. This is a base, not a full product — when in doubt,
build the simpler version (see `PLAN.md` §8 for what's explicitly out).

7A (backend, tasks 7.1–7.5) must land before 7B (frontend, 7.6–7.9) can
call real endpoints.

---

## Phase 7A — Backend

### Task 7.1 — Schema: `portal_credentials` and `sessions`

**Files:** `migrations/012_portal_credentials.sql`, `migrations/013_sessions.sql`.

```sql
-- 012_portal_credentials.sql
-- +goose Up
CREATE TABLE portal_credentials (
  account_id     TEXT PRIMARY KEY REFERENCES accounts(id),
  username       TEXT NOT NULL UNIQUE,
  password_hash  TEXT NOT NULL,
  role           TEXT NOT NULL CHECK (role IN ('customer','provider')),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE portal_credentials;
```

```sql
-- 013_sessions.sql
-- +goose Up
CREATE TABLE sessions (
  id_hash     BYTEA PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  role        TEXT NOT NULL CHECK (role IN ('customer','provider')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL,
  revoked_at  TIMESTAMPTZ
);
CREATE INDEX sessions_account_id_idx ON sessions(account_id);

-- +goose Down
DROP TABLE sessions;
```

Add matching `store.PortalCredential`/`store.Session` structs and a
`PortalAuth` sub-interface on `Store` (create credential, get by
username, create/get/revoke session).

**Verify:**
```sh
go run github.com/pressly/goose/v3/cmd/goose@v3.28.0 -dir migrations postgres "$LAZYCAKE_DATABASE_URL" up
go run github.com/pressly/goose/v3/cmd/goose@v3.28.0 -dir migrations postgres "$LAZYCAKE_DATABASE_URL" down
go run github.com/pressly/goose/v3/cmd/goose@v3.28.0 -dir migrations postgres "$LAZYCAKE_DATABASE_URL" down
go test ./internal/coordinator/store/...
```

### Task 7.2 — `internal/coordinator/portalapi`: signup, login, sessions

**Files:** new package: `session.go` (cookie issue/verify + middleware
resolving a request to account_id/role), `auth_handlers.go` (signup,
login, logout).

- `POST /api/portal/{customer,provider}/signup` — `{username, password}`.
  Reject if username taken or password < 8 chars. bcrypt-hash, create
  `Account` + `portal_credentials` row, create session, set cookie.
- `POST /api/portal/{customer,provider}/login` — `{username, password}`.
  `bcrypt.CompareHashAndPassword`; one generic error on any failure.
  Track failed attempts per username (in-memory map is fine at this
  scale) and reject with a short lockout after 10 in a row within a few
  minutes — `PLAN.md` §2's basic brute-force guard, nothing more.
- `POST /api/portal/logout` — revoke session, clear cookie.

Cookie: httpOnly, `Secure` unless `cfg.Dev`, `SameSite=Lax`, ~7 day
expiry, random 32-byte ID, hashed before any DB read/write.

**Done when:** signup → working session → logout → protected call 401s;
wrong password / unknown username both produce the same error; the
lockout kicks in after repeated bad attempts and clears after the window
passes.

**Verify:** `go test ./internal/coordinator/portalapi/...`

### Task 7.3 — Store scoping + event scoping

**Files:** `internal/coordinator/store/store.go` + implementation
(`ListNodesByAccount`, `ListTasksByAccount`), `internal/coordinator/events/events.go`
(`AccountID` field + account-scoped subscribe, every existing `Publish`
call site updated).

**Done when:** two seeded accounts each only see their own nodes/tasks
via the new queries; an account-scoped event subscriber only receives
its own account's events.

**Verify:** `go test ./internal/coordinator/store/... ./internal/coordinator/events/...`

### Task 7.4 — Customer portal endpoints

**Files:** `internal/coordinator/portalapi/customer_handlers.go`, behind
the task 7.2 session middleware.

- `GET /api/portal/customer/me` — balance/available balance
- `POST /api/portal/customer/tasks` — reuses `api.CustomerServer.SubmitTask`'s
  validation (don't duplicate the digest-pin/gateway-ownership/balance
  checks — call into the same logic, however that's cleanest once you're
  in the real code)
- `GET /api/portal/customer/tasks`, `GET /api/portal/customer/tasks/{id}`
- `GET /api/portal/customer/tasks/{id}/logs` — SSE, same polling shape
  `StreamLogs` already uses server-side
- `GET/POST /api/portal/customer/gateways`
- `GET /api/portal/customer/ledger`, `POST /api/portal/customer/balance/add`
  (dev-only, clearly named as such)

**Done when:** an integration test exercises every endpoint against real
Postgres, and a second account's session gets 404/empty for the first
account's resources.

**Verify:** `go test ./internal/coordinator/portalapi/...`

### Task 7.5 — Provider portal endpoints

**Files:** `internal/coordinator/portalapi/provider_handlers.go`.

- `GET /api/portal/provider/me` — lifetime earnings (sum of credit
  ledger entries)
- `GET /api/portal/provider/nodes`, `GET /api/portal/provider/nodes/{id}`
  (+ `ListTasksByNode`, already exists)
- `POST /api/portal/provider/nodes/install-token` — mints a fresh
  `agent`-kind API token, shown once, same contract as
  `CreateGateway`'s `install_token`
- `GET /api/portal/provider/ledger`

Then wire `portalapi` into `cmd/coordinator/run.go` on the existing
`httpServer` mux under `/api/portal/`.

**Done when:** a minted install token authenticates a real
`AgentService.Connect`; node/ledger listing is scoped per-account.

**Verify:** `go test ./internal/coordinator/portalapi/... ./internal/e2e/...`,
plus `curl localhost:8080/api/portal/customer/login` reachable against a
running compose stack.

---

## Phase 7B — Frontend

### Task 7.6 — Scaffolding + shared components

**Files:** `web/` at repo root (Vite + React + TypeScript + Tailwind,
two entry points or two small apps — whichever ends up cleaner),
`internal/coordinator/webassets` (`go:embed` of the build output), a
build script wired into `deploy/Dockerfile`'s `coordinator` stage (one
new `node:22-alpine` stage before the Go build).

Build the shared pieces as you need them, not as an upfront library:
nav shell, stat tile, data table, state badge (reuse the existing
`.state-*` color mapping from `internal/coordinator/dashboard/index.html`
as the source of truth), form inputs, a log viewer.

**Verify:**
```sh
cd web && npm ci && npm run build
podman build --target coordinator -t lazycake-coordinator-test -f deploy/Dockerfile ..
```

### Task 7.7 — Auth pages (both portals)

**Files:** signup/login pages, wired to task 7.2's endpoints.

**Done when:** register → land signed in → refresh stays signed in →
log out → protected route redirects to login, against a real running
coordinator.

### Task 7.8 — Customer portal pages

**Files:** dashboard, submit task, task detail (with live logs and live
state via SSE), task history, gateways, billing.

**Done when:** every action in `PLAN.md` §3's customer list works
end-to-end in a browser against `podman compose -f deploy/docker-compose.yml up`
— sign up, submit a real task, watch it go queued → running →
succeeded live, see it in history, create a gateway, see balance move
after settlement.

### Task 7.9 — Provider portal pages

**Files:** dashboard, add machine, machine detail, earnings.

**Done when:** sign up, add a machine, get a token + copy-pasteable
install snippet, actually start a real agent with it and watch it show
up connected within one heartbeat; machine detail shows real task
history and trust score; earnings reflects a real settled credit.

### Task 7.10 — Retire the old landing page; relocate `/ops`

**Files:** `internal/coordinator/dashboard/dashboard.go` (mount under
`/ops`), new `/` chooser page, `deploy/docker-compose.yml`, `README.md`,
`deploy/chaos-demo.sh`'s dashboard-URL comment.

**Done when:** `/` offers both portals, `/ops` behaves exactly as
before, and the full manual walkthrough (bring up the stack, submit a
task via the customer portal, add and connect a machine via the provider
portal, confirm `/ops` still shows the global view) passes; `go test
./...` and the frontend's own checks stay green.

---

## Appendix — endpoint summary

| Method | Path | Auth |
|---|---|---|
| POST | `/api/portal/{customer,provider}/signup` | none → session |
| POST | `/api/portal/{customer,provider}/login` | none → session |
| POST | `/api/portal/logout` | session |
| GET | `/api/portal/customer/me` | session |
| POST/GET | `/api/portal/customer/tasks` | session |
| GET | `/api/portal/customer/tasks/{id}` | session |
| GET | `/api/portal/customer/tasks/{id}/logs` | session, SSE |
| GET/POST | `/api/portal/customer/gateways` | session |
| GET | `/api/portal/customer/ledger` | session |
| POST | `/api/portal/customer/balance/add` | session |
| GET | `/api/portal/provider/me` | session |
| GET | `/api/portal/provider/nodes[/{id}]` | session |
| POST | `/api/portal/provider/nodes/install-token` | session |
| GET | `/api/portal/provider/ledger` | session |
| GET | `/api/portal/{customer,provider}/events` | session, SSE |
