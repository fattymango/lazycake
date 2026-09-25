# LazyCake Phase 7 — Provider and Customer Portals

Read `docs/00-core-platform/PLAN.md` and `docs/00-core-platform/IMPLEMENTATION.md`
first. This document is the design rationale for the next phase of work;
`IMPLEMENTATION.md` in this same directory is the task-by-task build order,
in the same style as the core platform's.

---

## 1. What's being built

Two separate, authenticated, role-specific web applications, replacing
today's single unauthenticated global dashboard:

- **Provider Portal** — for the person lending compute (today called a
  "host" in the code; this document uses "provider" for the human role and
  keeps "node"/"host account" for the existing data model terms). Register
  a machine, see it connect, watch what runs on it, see what it's earned,
  see its trust score and why it moved.
- **Customer Portal** — for the person paying for compute. Submit a task,
  watch it run with live logs, see task history, manage gateways, see
  balance and billing history, add funds.

Both are real product surfaces: sign in, see only your own data, take
action (submit a task, register a machine, create a gateway) without
touching a CLI. This is the first time either role gets a web UI at all —
today, submitting a task or registering a gateway means running `lcctl`,
and a provider has no UI whatsoever; they configure an agent token into an
env var and never look at a browser again.

The existing global dashboard (`internal/coordinator/dashboard`) is
platform-operator tooling — everyone's nodes, everyone's tasks, no
authentication — and stays exactly that: relocated, not replaced (§7).

## 2. Why this is a separate phase, not a tweak

The current dashboard is a single read-only HTML page with no login, no
per-account scoping, and no write actions — it was task 6.2's proof that
live state could be watched, not a product surface (see its own doc
comment: "no build step, no npm"). Two authenticated, role-separated,
fully interactive portals are a different kind of thing: they need a web
session layer that doesn't exist yet, REST/JSON endpoints for a browser
where today only gRPC exists (gRPC is what `lcctl` speaks; a browser
can't), per-account-scoped real-time updates where today's event bus is
one global unfiltered broadcast, and a frontend with actual navigation,
forms, and auth-gated routes where today there's one embedded HTML file.
None of this is a small addition to the existing dashboard package.

## 3. Non-goals (unchanged from the core platform, reconciled below)

`docs/00-core-platform/IMPLEMENTATION.md` §2 lists "User signup, OAuth,
password reset, email. Bearer tokens in a table" as an explicit non-goal.
This phase does **not** reverse that. It adds a *web session* on top of
the same bearer-token identity model — see §4's decision. Concretely,
still out of scope:

- Email verification, password reset flows, "forgot password" — there are
  no passwords.
- OAuth / social login / SSO.
- Real payment rails (Stripe, etc.) — "add funds" in the customer portal
  is the same kind of dev/demo balance adjustment the platform already
  has no real alternative to (`docs/00-core-platform/IMPLEMENTATION.md`'s
  "a credits ledger table is the whole billing system" non-goal stands).
- Multi-user accounts, teams, org hierarchies, RBAC beyond "customer
  role" vs "provider role." One account, one token-derived identity, one
  role, same as today.
- A general-purpose admin/support tool. The relocated ops dashboard (§7)
  is intentionally minimal.

## 4. Decision: how portal login works

**A portal "signs in" with the same bearer token the CLI already uses,
exchanged for a short-lived web session — not a new credential type.**

Concretely: a login screen has one field, "access token." The server
hashes it the same way `auth.Hash` already does, looks it up via the
existing `Store.Authenticate`, and — if it's a `customer` token on the
customer portal or an `agent`-account-owning identity on the provider
portal (see §4.1) — creates a session row and sets an httpOnly,
`Secure`, `SameSite=Lax` cookie referencing it. Every subsequent portal
page load and API call rides that cookie; the raw token is never stored
client-side beyond that one paste, and is never sent again after login.

This is deliberately **not**: a new username/password system, OAuth, or
anything requiring email. It's the existing "bearer tokens in a table"
model wearing a normal web session on top, which is the smallest change
that makes "professional dashboard" true without undoing a documented,
deliberate project decision. If a real product needs passwords or SSO
later, that's a distinct, bigger decision to make explicitly then — not
something to back into here.

**Self-serve account creation** follows the same shape the codebase
already uses for gateways (`CreateGateway` mints a gateway ID and an
install token, shown once). A "Create account" action on either portal's
login screen creates a new `Account` row and a fresh token of the right
kind, shows the token exactly once with a clear "save this, it's the only
way back in" warning, and starts a session immediately. No email, no
confirmation step — matches `seed.EnsureDemoAccount`'s own existing
pattern, just self-service instead of operator-run.

### 4.1 Provider identity is a gap the portal has to close

Today, an `agent` token authenticates the **agent process itself**
connecting to `AgentService.Connect` — there's no notion of a human
logging in as "the account that owns this node's tokens" anywhere. A
provider signing into the portal needs a token that identifies *them*,
not their running agent. Minting a fresh `agent`-kind token and pasting it
into a login box would work today (`Store.Authenticate` doesn't care that
the same kind is used for both), but it's confusing UX (a "login token"
and a "machine token" look identical and are easy to mix up) and means
handing a browser session the exact credential that lets a process
register as a node.

**Decision:** add a fourth `TokenKind`, `provider` (alongside `customer`,
`agent`, `gateway`), used only for portal login. A provider's first
"Create account" mints a `provider` token for the human, and a separate
"add a machine" action in the portal mints a fresh `agent` token per
machine (§6.2) — mirroring exactly how `CreateGateway` already separates
"create the gateway resource" from "the install token needed to run
`gateway`." An account can hold many `provider` tokens (multiple logins,
revocable independently) and many `agent` tokens (one per registered
machine).

## 5. Decision: a REST/JSON API layer for the browser

Today's only customer-facing API is gRPC (`CustomerService`), which
`lcctl` speaks natively but a browser cannot without a heavier bridge
(grpc-web plus a proxy, or full gRPC-Web codegen on the frontend). Given
this project's standing preference for the standard library over
frameworks (`net/http` throughout, no router package, no ORM), the
smallest-surface-area choice is a **new hand-written REST/JSON package**,
`internal/coordinator/portalapi`, that:

- Authenticates via the session cookie from §4 (not a bearer header —
  that's still how `lcctl` and machine-to-machine calls work; the portal
  is a different caller).
- Wraps the same `store.Store`, `billing`, `pricing`, and `scheduler`
  packages the gRPC servers already use — it is a second transport onto
  existing logic, not a reimplementation. `SubmitTask`'s validation (image
  must be digest-pinned, gateway ownership check, balance check) is
  reused directly by extracting the account-agnostic parts, so the two
  code paths can't drift into checking different things.
- Lives on the coordinator's existing HTTP port (`cfg.HTTPAddr`,
  today `:8080`), under `/api/portal/customer/*` and
  `/api/portal/provider/*`, alongside the relocated ops dashboard
  (`/ops`) and its own `/ops/api/state`. One HTTP listener, one port, no
  compose/deploy changes.

grpc-web was considered and rejected for this phase: it would mean a
build-time proxy (Envoy, or a Go grpc-web wrapper) plus generated
TypeScript client code, real complexity for a project that has otherwise
never needed anything beyond `net/http` and hand-rolled JSON. Revisit if
the REST surface's manual request/response structs become genuinely
painful to keep in sync — not a concern yet, given the entire API surface
this phase needs is under 20 endpoints (§6).

## 6. What each portal actually does

### 6.1 Customer portal

| Page | Backed by | Notes |
|---|---|---|
| Sign in / create account | new `portalapi` session endpoints | §4 |
| Overview | `AvailableBalance`, `ListTasksByAccount` (new, §8.2) | Balance, recent tasks, quick "submit task" |
| Submit task | `CustomerServer.SubmitTask`'s logic, reused | A form mirroring `lcctl submit`'s flags |
| Task detail | `GetTask` + `StreamLogs` (as SSE, not gRPC stream) | Live-updating state and log tail |
| Task history | `ListTasksByAccount` (new) | Filterable by state |
| Gateways | `CreateGateway`, `ListGateways`, reused | Same install-token-shown-once UX as `lcctl gateway create` |
| Billing | `LedgerEntriesForAccount` (already exists) | Charges over time; "add funds" (dev-mode balance bump) |

### 6.2 Provider portal

| Page | Backed by | Notes |
|---|---|---|
| Sign in / create account | new `portalapi` session endpoints | §4 |
| Overview | `ListNodesByAccount` (new, §8.2), `LedgerEntriesForAccount` | Connected machines, lifetime earnings |
| Add a machine | new: mint an `agent` token, show install command | The gap in §4.1 — today this only happens via `seed`/direct DB access |
| Node detail | `GetNode`, `ListTasksByNode` (already exists), trust score | What ran here, current trust score and why (reuses the delta table `docs/00-core-platform/README.md` already documents) |
| Earnings | `LedgerEntriesForAccount`, filtered to `kind = credit` | Per-task and running totals |

Both portals get real-time updates (new task lands, node connects, trust
score moves) over the same SSE mechanism task 6.1 already built, scoped
per-account (§8.3) rather than broadcast to everyone.

## 7. The existing dashboard doesn't go away — it moves

`internal/coordinator/dashboard` stays as **platform-operator tooling**:
global, cross-account visibility that neither a customer nor a provider
should have, and that an operator running the platform still genuinely
needs (is the fleet healthy, is any node's trust cratering, what's the
platform-wide charge/credit total). It moves from `/` to `/ops`, keeps
its current no-auth-required posture explicitly documented as
"operator-network-only, not internet-facing" (it already has no auth
today; this phase doesn't add any either, since it's out of scope — see
`docs/01-dashboard-portals/OPEN_QUESTIONS.md`), and `/` becomes a simple
chooser page ("Provider" / "Customer" sign-in links) once both portals
exist.

## 8. Backend changes this phase requires

Small, additive, no schema changes to existing tables:

1. **`sessions` table** (new migration `012_sessions.sql`): `id`,
   `account_id`, `kind` (`customer` | `provider`, mirroring which portal
   it's valid for), `created_at`, `expires_at`, `revoked_at`. A session
   is looked up by a random ID stored in the cookie, hashed the same way
   tokens are — never the raw ID at rest, consistent with how
   `api_tokens` already never stores a raw token.
2. **`provider` added to the `api_tokens.kind` CHECK constraint**
   (migration `013_provider_token_kind.sql`) — see §4.1.
3. **`Store.ListNodesByAccount(ctx, accountID)`** and
   **`Store.ListTasksByAccount(ctx, accountID, limit)`** — both trivial
   `WHERE account_id = $1` queries alongside the existing `ListNodes`/
   `ListRecentTasks`, needed because neither portal should ever see
   another account's rows and no such scoped query exists today.
4. **`events.Event` gains an `AccountID` field**, populated at every
   existing `Publish` call site (the publisher already knows the account
   — it's the task's or node's own). The SSE handler filters to the
   caller's own account before writing, rather than broadcasting
   unfiltered as it does today (`dashboard.Server.handleEvents`) — that
   behavior is fine for `/ops` (operators are meant to see everything)
   but wrong for a customer or provider endpoint.
5. **`internal/coordinator/portalapi`** (§5): sessions, the REST
   endpoints in §6's tables, and the account-creation flow in §4.

No changes to `AgentService`, `GatewayService`, the scheduler, billing
math, or the tunnel — this phase is additive at the edges, not a rework
of anything load-bearing.

## 9. Frontend

### 9.1 Decision: a real build step, embedded the same way

The current page's "no build step, no npm" was the right call for one
static status page. Two multi-page, auth-gated, form-heavy applications
with live updates are past where hand-written vanilla JS stays
maintainable or looks professional. This phase adds:

- **React + TypeScript + Vite**, one frontend project with two
  entry points (`provider` and `customer`) sharing a component library —
  not two separate codebases, since the visual language and most
  primitives (nav shell, stat tiles, tables, forms, toasts) are identical
  and only the pages/data differ.
- **Tailwind CSS** for styling, so the design system (§9.2) is enforced
  through configuration (a token palette, spacing scale) rather than
  hand-maintained CSS files drifting apart the way even the current
  single dashboard's inline `<style>` block would if copy-pasted twice.
- Built assets are `go:embed`'d into the coordinator binary exactly the
  way `index.html` is today — **the single-binary deploy story does not
  change.** Node/npm becomes a build-time dependency (CI and local dev),
  never a runtime one; `deploy/Dockerfile`'s `coordinator` stage gains a
  `node:22-alpine` build stage before the existing Go build stage, same
  multi-stage pattern already used for `build`/`coordinator`/`agent`.

This is the one new toolchain dependency in an otherwise Go-only repo,
worth stating plainly rather than sneaking in: it's the standard,
professional way to build something with this much interactive surface,
and it costs nothing at runtime or deploy time.

### 9.2 Design direction

Professional, not playful — this is billing/infrastructure software.
Concretely:

- **Layout:** persistent left sidebar (nav + account/balance summary) +
  top bar (page title, live-connection indicator, sign out) + content
  area. Both portals share this shell; only the nav items and content
  differ.
- **Color:** one neutral base (near-black text on near-white background
  in light mode; the existing dashboard's dark palette as the dark-mode
  counterpart, not a second design) plus one accent color and the
  existing semantic state colors already chosen for task states
  (`--good`/`--warn`/`--bad` etc. in `index.html`) — reused, not
  reinvented, so state colors mean the same thing in the ops dashboard
  and both portals.
- **Data-dense, not sparse:** tables with real pagination/filtering for
  task and ledger history, stat tiles for the handful of numbers that
  matter (balance, active tasks, connected nodes, lifetime earnings), no
  decorative illustration or marketing-site styling.
- **Every async state handled explicitly:** loading skeletons, empty
  states with a clear next action ("no machines yet — add one"), and
  error states that show what failed, not a blank page — the current
  dashboard has none of these because it only ever shows a fully-loaded
  global snapshot.
- Component inventory needed (built once, used by both portals): nav
  shell, stat tile, data table (sortable, paginated), state badge (reuse
  existing state→color mapping), form inputs with inline validation,
  toast/inline error, copy-to-clipboard (for tokens/IDs), live-log
  viewer (monospace, auto-scroll, reused for task log streaming).

## 10. Security notes for this phase specifically

- **CSRF:** cookie-based sessions need it where bearer tokens didn't —
  `SameSite=Lax` plus a custom header the frontend sets on every mutating
  request (checked server-side) covers this without a stateful
  double-submit token.
- **Session fixation/rotation:** issue a new session ID on login, never
  reuse one from an unauthenticated request.
- **Token display:** the one-time token/session reveal (§4) must be
  copy-clickable and explicitly warn it won't be shown again, matching
  `CreateGateway`'s existing `install_token` UX — no new pattern to
  design, just extend the existing one to the browser.
- **Still no rate limiting anywhere in this codebase** (true today too,
  for gRPC bearer auth) — noted as a real gap in
  `docs/01-dashboard-portals/OPEN_QUESTIONS.md`, not silently deferred.

## 11. Relationship to phases 0–6

This is Phase 7. It depends on nothing changing in phases 0–6's own
behavior — dispatch, leases, billing math, the tunnel, and trust scoring
are all read from, never modified. `docs/00-core-platform/PROGRESS.md`
keeps logging phase 0–6 work; this phase's own progress log is
`docs/01-dashboard-portals/PROGRESS.md`, created once work actually
starts (not pre-filled here — see this directory's `IMPLEMENTATION.md`
§0).
