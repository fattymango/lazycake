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

## Left for the implementer

- **Task 7.4:** whether `portalapi` calls into `api.CustomerServer`'s
  logic via an extracted shared helper or direct method calls with a
  constructed context — depends on what the real code looks like once
  you're in it. Fixed constraint either way: no duplicated validation.
- **Task 7.2:** exact shape of the failed-login lockout (in-memory map
  keyed by username is the suggested starting point — simplest thing
  that stops naive brute-forcing, not meant to survive a coordinator
  restart or scale past one replica).

## Frontend built ahead of the backend (7.6-7.9 before 7A)

Explicit project-owner direction, overriding `IMPLEMENTATION.md`'s own
stated order ("7A must land before 7B can call real endpoints"): build the
Phase 7B frontend now, with Phase 7A (`portalapi`, tasks 7.1-7.5) not yet
started. Consequences worth flagging rather than discovering later:

- **API surface the frontend assumes** (`web/src/shared/types.ts`,
  `api.ts`): built from `PLAN.md`'s Appendix table and
  `internal/coordinator/store/types.go`'s existing Go structs, not from a
  real handler. Where the plan's endpoint list didn't fully specify a
  shape, the frontend picked the simplest reasonable one — check these
  against whatever 7A actually implements:
  - `GET /api/portal/customer/me` returns `balance_micros` *and*
    `available_balance_micros` (the plan's "balance + available balance");
    `GET /api/portal/provider/me` returns `lifetime_earnings_micros`.
  - `POST /api/portal/customer/tasks` request/response both assumed to be
    close to `store.Task`/its create request, flattened to snake_case the
    same way `dashboard`'s `taskView` flattens `store.Task` today.
  - `GET /api/portal/customer/tasks/{id}/logs` is assumed to be an SSE
    stream of individual `LogLine`s (one JSON object per `data:` line,
    matching `store.LogLine`), not a one-shot JSON array — "same polling
    shape `StreamLogs` already uses server-side" read as "stream it", not
    "return a snapshot".
  - `GET /api/portal/provider/nodes/{id}/tasks` — **invented**, not in the
    plan's endpoint table. `IMPLEMENTATION.md` task 7.5 says machine detail
    needs `ListTasksByNode` (which already exists at the store layer) but
    the Appendix only lists `GET /api/portal/provider/nodes[/{id}]`. Either
    fold recent tasks into the node-detail response, or confirm this
    sub-path, when building 7.5.
  - `POST /api/portal/customer/balance/add` assumed to take
    `{amount_micros}` and return nothing meaningful (frontend just re-fetches
    `/me` and `/ledger` after).
  - Account-scoped SSE (`GET /api/portal/{role}/events`) assumed to emit
    the same shape as the existing fleet-wide `/events` (task 6.1's
    `events.Event`), just filtered to the caller's account — see
    `PortalEvent` in `types.ts`.
- **`web/`'s own build has not been run or verified** — no npm registry
  access in this sandboxed session. `deploy/Dockerfile`'s new `web-build`
  stage and `internal/coordinator/webassets`'s embed were checked for
  internal consistency (`go build ./...` / `go vet` pass with an empty
  placeholder `dist/`) but `npm ci && npm run build` itself has not
  actually been executed anywhere. Run it as the very first check before
  relying on anything else here.

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
