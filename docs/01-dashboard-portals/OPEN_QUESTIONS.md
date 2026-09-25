# Phase 7 (Provider/Customer Portals) — Open Questions

Judgment calls and real gaps, documented honestly rather than silently
decided or silently skipped — same spirit as
`docs/00-core-platform/OPEN_QUESTIONS.md`.

## Decided, flagged for visibility

- **Username/password auth, overriding this plan's own first draft**
  (which proposed a token-exchanged session instead). Explicit project
  owner direction: simple auth, username and password only, no email, no
  OAuth. Partially reverses
  `docs/00-core-platform/IMPLEMENTATION.md` §2's "bearer tokens in a
  table" non-goal for portal login specifically — `lcctl` and every
  machine credential (agent/gateway/customer API tokens) are unaffected.
  See `PLAN.md` §2.
- **This is a base, not a full product** — explicit project owner
  direction after the first draft of this plan leaned toward a fuller
  security/design-system treatment. `PLAN.md` §8 lists what's explicitly
  cut (teams/orgs, rate-limiting infra, component library ceremony, a
  dedicated accessibility workstream, session-management UI). If actual
  usage later demands any of these, that's a future phase, not a sign
  this one was done wrong.
- **REST/JSON over grpc-web** (`PLAN.md` §4). Right call for ~15
  endpoints; revisit only if the API surface grows substantially.
- **React + TypeScript + Vite + Tailwind** (`PLAN.md` §6), the one new
  toolchain dependency in an otherwise Go-only repo, embedded via
  `go:embed` so the single-binary deploy story is unchanged.

## Left for the implementer — resolved, once actually implemented

- **Task 7.4:** `portalapi` calls into `api.CustomerServer` via a new
  exported `SubmitTaskForAccount` extracted from the gRPC `SubmitTask`'s
  body (transport-agnostic params in, `*status.Error` out either way) -
  the "direct method calls with a constructed context" option, since
  `SubmitTask` already had `tok.AccountID` as its only real dependency on
  the gRPC-specific auth path once separated out. No duplicated
  validation: `customer_server_test.go`'s existing digest-pin/gateway-
  ownership tests exercise the exact same code either RPC or HTTP calls
  into.
- **Task 7.2:** the lockout is exactly the suggested in-memory map keyed
  by username (`portalapi/lockout.go`), 10 failures in 5 minutes locking a
  username out for 5 minutes. As flagged when this was only a plan: it
  does not survive a coordinator restart and does not coordinate across
  replicas - unchanged scope, now implemented rather than just described.

## Frontend built ahead of the backend (7.6-7.9 before 7A) — reconciled

The frontend (7.6-7.9) was built before the backend (7.1-7.5) existed, on
explicit project-owner direction, against a best-effort reading of
`PLAN.md`'s Appendix table. Now that 7A is implemented, here's how that
guess actually turned out - checked field-by-field against
`internal/coordinator/portalapi`'s real wire shapes:

- **Matches exactly, no changes needed on either side:** `GET .../me` for
  both roles, `POST/GET .../tasks`, `GET .../tasks/{id}`, gateway list/create,
  ledger entries, node list/detail, install-token response, and the SSE
  log stream's per-line shape. The frontend's guesses at
  `available_balance_micros`, `lifetime_earnings_micros`, and a flattened
  snake_case task/node/gateway view (mirroring
  `internal/coordinator/dashboard`'s own existing convention) all landed
  correctly.
- **`GET /api/portal/provider/nodes/{id}/tasks`** - the frontend's own
  invented endpoint (not in the plan's Appendix table) - was implemented
  exactly as guessed: its own route rather than folding into node detail,
  so a long task history doesn't inflate every node-detail response.
- **Known, documented deltas, neither side wrong so much as not fully
  specified by the plan:**
  - `taskView` never sets `price_micros` - the frontend's `Task` type
    marks it optional and the UI already renders "—" for `undefined`, so
    this is a real gap (a settled task's actual price isn't surfaced yet)
    rather than a breaking mismatch. Closing it means joining
    `task_meters`/pricing into `toTaskView`, not done here.
  - The account-scoped SSE stream emits `events.Event`'s real shape
    verbatim (`task_state`, `node_connected`, `node_disconnected`,
    *and* `capacity` - the frontend's `PortalEvent` union doesn't model
    `capacity`, but an unrecognized `type` is silently ignored by its
    `switch`, so this is harmless dead data, not a bug). The frontend also
    expects a `"balance"` event type the backend never emits - there is no
    such `events.Event` type, and nothing currently publishes one on
    `AdjustBalance`; a balance change is only ever picked up by the
    frontend's own re-fetch after an action it just took itself, not
    pushed live. Worth adding if a portal ever needs to reflect another
    session's balance change (e.g. two tabs open) without a manual
    refresh.
  - `POST /api/portal/customer/balance/add` is **not** gated behind
    `cfg.Dev` at the handler level - it exists and works in every
    deployment, clearly labeled dev-only in its doc comment and in the
    frontend's own UI copy, but nothing stops it from being hit in a
    real deployment today. Whether it should 404 outside `cfg.Dev` (and
    who decides "dev" at the HTTP layer vs. relying on network
    exposure/documentation) is a deployment decision, not something baked
    into `portalapi` itself - flagging it rather than deciding it here.

## Real gaps this base does not close

- **No password recovery.** Lose your password, lose the account —
  same practical consequence as losing a bearer token today, just more
  likely to surprise someone now that there's a familiar-looking login
  screen. The registration page must say this plainly (`PLAN.md` §2).
- **No rate limiting beyond the one login-lockout in task 7.2** — true
  of the existing gRPC bearer auth too (a pre-existing gap, not a
  regression), but worth naming since a password is more guessable than
  a 32-byte token.
- **No session-management UI** (view/revoke other sessions) — deliberately
  cut (`PLAN.md` §8). `RevokeSession` exists at the store layer for the
  logout flow itself; nothing else calls it.
- **The install command assumes a locally-built `lazycake-agent` image**
  (`podman build --target agent -f deploy/Dockerfile -t lazycake-agent .`)
  and a real, reachable `LAZYCAKE_PUBLIC_GRPC_ADDR`. There is no published
  image registry, so a true one-line "copy, paste, done" self-service story
  needs one - a deployment decision, not something to invent here.
- **`go test -tags=integration ./...` is not safe to run with Go's default
  package parallelism** now that two packages (`store`, `portalapi`) share
  one real Postgres database and both truncate it per test. Use `-p 1`
  (sequential packages) or accept that `make test-integration` may
  intermittently fail with a row that "shouldn't" be missing - a
  pre-existing characteristic of `store`'s own test convention, only now
  visible because `portalapi` is the second integration-tagged package
  to use it. Not fixed here (see `PROGRESS.md`'s 7.1-7.5 entry) - real
  isolation would mean per-test transactions or per-package schemas.
