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
