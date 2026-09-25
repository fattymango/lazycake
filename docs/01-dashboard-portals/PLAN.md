# LazyCake Phase 7 — Provider and Customer Portals

Read `docs/00-core-platform/PLAN.md` and `docs/00-core-platform/IMPLEMENTATION.md`
first. This document is the design rationale for the next phase of work;
`IMPLEMENTATION.md` in this same directory is the task-by-task build order.

**Scope note:** this is a base — two working, professional-looking portals
covering the core actions each role needs, not a fully-featured SaaS
product. No teams/orgs, no plans/tiers, no admin console, no elaborate
security program. Where a decision could go simple or elaborate, this
plan picks simple and says so.

---

## 1. What's being built

Two separate, authenticated, role-specific web apps, replacing today's
single unauthenticated global dashboard:

- **Provider Portal** — for the person lending compute. Register a
  machine, watch it connect, see what ran on it and what it earned.
- **Customer Portal** — for the person paying for compute. Submit a
  task, watch it run with live logs, manage gateways, see balance and
  billing history.

Today, neither role has a web UI at all — everything is `lcctl` or, for a
provider, not even that (they configure an agent token into an env var
and never see a browser). The existing global dashboard
(`internal/coordinator/dashboard`) is unauthenticated, shows everyone's
data, and has no write actions; it stays as operator-only tooling,
relocated to `/ops` (§6).

## 2. Auth: username and password, kept simple

Registering for either portal takes a username and a password; signing
in takes the same two fields. No email, no OAuth, no password reset —
losing your password loses the account, and the registration screen says
so plainly.

This is a deliberate, explicit reversal of one line in
`docs/00-core-platform/IMPLEMENTATION.md` §2 ("no user signup... bearer
tokens in a table"), called out rather than glossed over. What it does
**not** touch: `lcctl` and every machine-to-machine caller (agents,
gateways) keep using the existing bearer tokens in `api_tokens`,
completely unrelated to this. A password authenticates a human in a
browser; a bearer token authenticates a process. Two separate, unrelated
mechanisms, no shared code path.

Mechanics: a new `portal_credentials` table (account_id, unique username,
bcrypt password hash, role — `customer` or `provider`, fixed at signup by
which portal you registered on). Login hashes-and-compares
(`golang.org/x/crypto/bcrypt`, already pulled in transitively by the
existing Noise dependency — no new third-party package), then issues an
httpOnly `SameSite=Lax` session cookie referencing a random ID stored
hashed, same pattern `api_tokens` already uses for tokens. One generic
"invalid username or password" error on any failure — don't leak which
usernames exist.

Minimum password length: 8 characters. Nothing fancier. Basic
brute-force protection (a per-username failed-attempt counter that
briefly locks out after, say, 10 failures) is worth doing since a
human-chosen password is realistically guessable in a way a 32-byte
random token never was — kept as one small piece of task 7.2, not a
dedicated security workstream.

## 3. What each portal does

**Customer:** sign up/log in, dashboard (balance + recent tasks), submit
a task, task detail with live logs, gateways (list/create — this already
exists as `CustomerService` RPCs; the portal is a UI on top, not new
logic), billing (ledger history + a dev-only "add funds" button, since
there's no real payment rail and none is being added here).

**Provider:** sign up/log in, dashboard (connected machines + lifetime
earnings), "add a machine" (mints an `agent` API token + shows the
install command — closes the real gap where this currently has no
self-service path at all), machine detail (recent tasks, trust score),
earnings (ledger history).

Both get live updates on their own dashboard via the SSE mechanism task
6.1 already built, scoped to the signed-in account (today it broadcasts
unfiltered to everyone, which is fine for `/ops` but wrong for a portal).

## 4. API layer: plain REST/JSON, reusing existing logic

A new `internal/coordinator/portalapi` package, hand-written REST/JSON
(not grpc-web — the existing project has never needed anything beyond
`net/http`, and ~15 endpoints doesn't justify a build-time proxy and
generated client code). It wraps the same `store.Store`/`billing`/
`pricing` logic the gRPC servers already use rather than reimplementing
task submission, balance checks, etc. a second time. Lives on the
coordinator's existing HTTP port under `/api/portal/*`, alongside the
relocated `/ops` dashboard — one listener, no new ports, no compose
changes.

## 5. Backend changes

1. **`portal_credentials` table** (migration `012_...sql`): account_id,
   username (unique), password_hash, role, created_at.
2. **`sessions` table** (migration `013_...sql`): id_hash, account_id,
   role, created_at, expires_at, revoked_at — looked up by a hashed
   random ID from the cookie, same pattern as `api_tokens`.
3. **`Store.ListNodesByAccount`** and **`Store.ListTasksByAccount`** —
   trivial `WHERE account_id = $1` queries; don't exist today (only
   fleet-wide `ListNodes`/`ListRecentTasks` and per-node
   `ListTasksByNode` do), and both portals need them so nobody ever sees
   another account's rows.
4. **`events.Event` gains `AccountID`**, set at every existing `Publish`
   call site, so the SSE handler can filter to the caller's own account
   instead of broadcasting everything (fine for `/ops`, wrong for a
   portal).
5. **`internal/coordinator/portalapi`** itself (§4): auth (§2) plus the
   endpoints in §3.

No changes to `AgentService`, `GatewayService`, the scheduler, billing
math, or the tunnel.

## 6. Frontend

One React + TypeScript + Vite project, two entry points (provider,
customer) sharing a small set of components (nav shell, stat tile, data
table, state badge reusing the existing state→color mapping from
`internal/coordinator/dashboard/index.html`, form inputs, a log viewer).
Tailwind for styling. Built assets `go:embed`'d into the coordinator
binary exactly like `index.html` is today — the single-binary deploy
story doesn't change; Node/npm is a build-time dependency only
(`deploy/Dockerfile` gains one build stage).

This is the one new toolchain dependency in an otherwise Go-only repo.
The current page's "no build step" was right for one static status page;
two auth-gated, multi-page, form-heavy apps are past where hand-written
vanilla JS stays maintainable, which is a real driver of the "school
project" look this phase is meant to fix.

Visual direction: clean and minimal, not decorative — sidebar nav + top
bar + content, one neutral palette plus the existing semantic state
colors, real empty/loading/error states instead of only ever showing a
fully-loaded snapshot (the current dashboard's only mode). No design
system document beyond this paragraph; build the handful of shared
components as they're needed.

## 7. The existing dashboard moves, doesn't disappear

`internal/coordinator/dashboard` becomes operator-only tooling at `/ops`
— still no auth (out of scope for this phase, same as today), still
global. `/` becomes a two-link chooser ("Provider" / "Customer").

## 8. Explicitly not in this base

- Teams, orgs, multi-user accounts, RBAC — one username, one role.
- Any real security program beyond bcrypt + basic lockout: no rate
  limiting infra, no audit log, no 2FA.
- A component library, design tokens doc, or Storybook — a handful of
  shared React components is enough.
- A dedicated accessibility/responsive workstream — pages should
  reasonably work on a phone and a keyboard as they're built, not as a
  separate certification pass.
- Session management UI (view/revoke other sessions) — log in, log out,
  that's it.
- Password reset, email, OAuth (§2).

## 9. Relationship to phases 0–6

This is Phase 7, purely additive — nothing in phases 0–6's behavior
changes. `docs/01-dashboard-portals/PROGRESS.md` is created once work
actually starts, same convention as the root project.
