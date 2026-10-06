# LazyCake Phase 8 — Frontend Overhaul: Implementation Guide

Read `PLAN.md` in this directory first. Same rules as
`docs/00-core-platform/IMPLEMENTATION.md` §0: one task at a time, verify
before moving on, commit as `8.<task>: <description>` (plain messages, no
`Co-Authored-By` trailer — see repo `HANDOVER.md`), keep `PROGRESS.md`
updated, log judgment calls in `OPEN_QUESTIONS.md`. Visual tasks are not done
until screenshots of the result have been inspected (task 8.10's script).

---

## 8A — Root-cause fixes (server)

### Task 8.1 — SPA fallback no longer redirect-loops

**Files:** `internal/coordinator/webassets/webassets.go` (+ test).
Serve `index.html`'s bytes directly for any non-asset path; never route the
fallback through `http.FileServer`. `Cache-Control: no-cache` on the HTML,
`public, max-age=31536000, immutable` on `/assets/*`. Unknown `/api/*` paths
must still 404 as JSON, not return the shell.
**Verify:** a Go test requesting `/tasks/tsk_x`, `/gateways`, `/` and
`/assets/<file>` gets 200 with the right body and no redirect; a request for
`/api/portal/nope` is not the shell.

### Task 8.2 — Log stream resumes instead of replaying, and says when it's done

**Files:** `internal/coordinator/portalapi/customer_handlers.go` (+ test).
Emit `id: <seq>` per frame; start from `Last-Event-ID` when present; when the
task is terminal and drained send `event: end\ndata: {}\n\n` then close.
**Verify:** a test streams a terminal task's logs, reconnects with
`Last-Event-ID`, and receives nothing new; the first stream ends with the
`end` event.

## 8B — Design foundation

### Task 8.3 — Tokens, fonts, Tailwind mapping, icons

**Files:** `web/src/ui/tokens.css`, `web/tailwind.config.js`, `web/package.json`.
Semantic CSS variables for dark and light (`:root`, `[data-theme=light]`,
OS-preference default), Tailwind colors mapped to `rgb(var(--…) / <alpha>)`,
`@fontsource-variable/inter` + `jetbrains-mono`, `lucide-react`, reduced
motion, scrollbar and selection styling. **Verify:** build passes; no raw hex
outside `tokens.css`.

### Task 8.4 — Primitives

**Files:** `web/src/ui/*`. Button (variants, sizes, loading), IconButton,
Input/Textarea/Select/Switch/Field, Card, Badge, Skeleton, Spinner, Tooltip,
Dialog, DropdownMenu, Tabs (Radix-backed), Toast system, `cn()` helper.
**Verify:** a `/_kit` dev-only route renders every primitive in both themes.

### Task 8.5 — Display components

**Files:** `web/src/ui/*`. `TruncatedText`/`Identifier` (ellipsis, tooltip,
copy), `CodeBlock` (copy), `StatCard` with sparkline, `StatusPill` +
`status.ts` registry, `DataTable` (sticky head, skeleton, empty, truncation,
row links, optional sort), `KeyValueList`, `EmptyState`, `ErrorState`,
`PageHeader`, `Section`, `ProgressBar`. **Verify:** `/_kit` shows each with
an adversarial 120-character unbroken ID and nothing overflows.

### Task 8.6 — App shell and resilience

**Files:** `web/src/ui/AppShell.tsx`, `ErrorBoundary.tsx`, `NotFound.tsx`.
Sidebar (grouped, collapsible, mobile drawer), top bar (breadcrumbs, live
indicator, theme toggle, user menu), error boundary around routes, designed
404, session-expiry handling (a 401 anywhere routes to login with the
return path), offline banner. **Verify:** killing the API shows the designed
error state, not a blank page; an unknown route shows the 404.

## 8C — Pages

### Task 8.7 — Auth

Login and signup: split layout with brand panel, inline validation, role
choice on signup, clear errors, password visibility toggle.

### Task 8.8 — Customer portal

Dashboard (balance, recent tasks, running/queued counts, sparkline),
Submit task (presets, env-var editor, tunnel-target picker, resource
sliders with live cost hint), Tasks (search, state filter, pagination),
Task detail (header with status + actions, tabs: Overview / Logs / Spec,
lifecycle timeline, the new log viewer, end-aware stream per 8.2),
Gateways (cards, connection state, create dialog with one-time token reveal),
Billing (balance, add funds, ledger table).

### Task 8.9 — Provider portal

Dashboard (fleet summary, earnings, machine cards with capacity bars),
Add machine (stepper, copyable install command with the corrected image
name and notes), Machine detail (status, capacity, trust score, recent tasks,
remove), Earnings (totals + ledger).

## 8D — Verification and handover

### Task 8.10 — Visual and behavioural verification

**Files:** `web/e2e/` (puppeteer-core), `web/e2e/seed.sql`, README section.
Seed a local coordinator + Postgres; capture every page × {390, 820, 1440}px
× {dark, light}; overflow audit; the deep-link-refresh and log-stream tests
from `PLAN.md` §5. **Verify:** all screenshots reviewed, audit clean, tests
green; screenshots of the final product committed under `docs/screenshots/`
for the README.

### Task 8.11 — Cleanup and docs

Delete the old components and styles, update `docs/README.md` and the
top-level README, record the design system's extension points (how to add a
page, a status, a theme) in `web/README.md`.

---

## Queued: do after the UI has been signed off

### Task 8.12 — Stop a task (customer-initiated cancel)

**Status: DONE 2026-10-07** (see "As built" below). Requested 2026-10-07 and built once the owner signed off the UI. Nothing in the product can stop a task on request today: the old UI never had a stop button, there is no
portal endpoint, no `CustomerService` RPC and no `lcctl` command. What exists is the plumbing underneath:
`store.TaskCancelled`, the agent's handling of the coordinator's `Cancel` message
(`executor.CancelTask`, already used by task adoption on reconnect) and `terminalState("cancelled")`.

**Build:**
1. `POST /api/portal/customer/tasks/{id}/cancel` (account-scoped like the other task routes; 404 for someone else's
   task; 409 if already terminal). A `CancelTask` RPC on `CustomerService` and `lcctl cancel <id>` so the CLI and the
   portal share one code path (`api.CustomerServer`), as submit does.
2. Behaviour by state: `queued`/`reserved` -> mark `cancelled` directly and release any capacity hold; `dispatched`/
   `running` -> send `Cancel` to the node and let its `TaskFinished{"cancelled"}` settle it. A node that is gone must
   not strand the task: fall back to the lease-reclaim path.
3. Billing: the balance hold is released and the customer is charged only for time actually used; the provider is
   credited for the same (see the payout rules in `00-core-platform/PLAN.md`). Cancelling must be idempotent.
4. UI: a "Stop task" button on the task page header (visible only while the task is active) with a confirmation
   dialog, optimistic state, and the existing live `task_state` event to confirm; disabled with a spinner while the
   request is in flight; an error toast if it fails.

**As built (where it differs from the sketch above, and why):**
- A customer's stop is its own exit reason, **`stopped`** (state `cancelled`), distinct from the coordinator's `cancelled`. The billing
  rules deliberately never pay for a coordinator cancel, but a customer's stop is real work the host did, so `stopped` is billable
  (`billing.billable`), while `cancelled` is unchanged. The agent reports both as "cancelled"; the scheduler tells them apart by the new
  `tasks.cancel_requested_at` column (migration 016) and rewrites the reason before billing, so there is still exactly one settlement path.
- Queued: cancelled on the spot (transition conditional on `queued`; if a node claimed it in that instant it re-reads and takes the active
  path). Reserved/dispatched/running: `RequestTaskCancel` records the request (only while the task is still active, so a task that finished
  in the same instant is never marked) and the node is nudged immediately.
- **The scheduler keeps nudging**: a node still pulling the image can't act on a Cancel, and one lost on a busy stream is never seen, so
  `resendCancels` re-sends every 5s until the task finishes (the agent treats an unknown/finished task as a no-op).
- **A node that has gone quiet**: the reclaimer finishes a cancel-requested task as `stopped` (billing the time it ran if it had started)
  instead of requeueing it elsewhere or abandoning it as a vanished host.
- A task that finishes by itself in the instant of the request settles as a normal completion (the request changes nothing).
- Idempotent: stopping an already-stopped task succeeds; stopping a finished one is `FAILED_PRECONDITION` (HTTP 422); someone else's task is NotFound.
- Surfaces: `POST /api/portal/customer/tasks/{id}/cancel`, gRPC `CustomerService.CancelTask`, `lcctl cancel <id>` (`lcctl status` shows
  "stopping"), a "Stop task" button with a confirmation dialog on the task page, a "Stopping" status (header, tables, timeline) and
  "Stopped by you" wording throughout.

**Verified:** unit tests (every state, idempotency, ownership, offline node); Postgres integration tests for the store query, the scheduler
(rewrite, throttled resend, dead-node finish, a race where the task completes anyway) and the HTTP endpoint; a billing test that a stop charges
exactly the price of the time used and credits the host (and a mutation check that breaking either rule makes the tests fail); a real
end-to-end test (`internal/e2e/stop_test.go`) with a real agent and podman: a 5-minute task is stopped, the container is really gone, the
customer is charged for the time it ran, the host is paid, the hold is released, a repeat doesn't bill twice; and the full UI flow in a
real browser against a real agent (Stopping shown in 38 ms, "Stopped by you" 198 ms after the click, 0 containers left, $0.000143 charged).

**Original verification list:** cancel a queued task; cancel a running
task and check the container is really gone on the node; cancel a task that is finishing in the same instant (no double
settlement, no negative balance); cancel twice; cancel someone else's task (404); cancel while the node is offline;
the ledger and balance afterwards. Add the e2e check to `web/e2e/behaviour.mjs`.

### Task 8.13 — Gateway "Test connection" (with a legend)

**Status: DONE 2026-10-07** (see "As built" below). Suggested 2026-10-07. 8.13 and 8.14 both change the gateway binary, so 8.14 should ship
in the same gateway release if it follows soon.

**Why.** Misconfigured gateways are the likeliest first-run failure and today the only symptom is a task dying with
"bad address" or a reset. A gateway has two independent layers that fail separately: the gateway program (connected to our
relay, shown as Online/Offline) and each published service (a port on the gateway's own machine, e.g. `postgres:5432`). A test
that only checked the first would show green exactly when a service is dead, so it must check both.

**Result states (the headline above a per-service checklist):**
- **Green** — gateway connected and every published service reachable.
- **Yellow** — gateway connected but at least one service fails (also "0 of N reachable"; the checklist names the failing ones).
- **Red** — the gateway itself isn't connected, so there is nothing to test. (Deliberately *not* "nothing reachable": a connected
  gateway with every service refusing is a service-side fix and shows yellow with a count.)
- **Grey** — connected, but an old gateway without the probe, so services can't be verified: "Connected, services not
  verified. Update your gateway for a full test". Never green: green must mean verified.

**Build:**
1. Backend `POST /api/portal/customer/gateways/{id}/test`: returns `{gateway: {connected, rtt_ms}, services: [{name, port, ok,
   error, ms}], verified: bool}`. Hard server-side timeout (~5s) so a dead gateway can't hold a request open. **No server-side
   rate limiting** (decided: keep it simple; one authenticated user, one cheap probe per click).
2. Gateway-side probe: a relayed stream whose header carries a probe marker; the gateway dials the local service and answers a
   plaintext `{ok, error}`. It carries no customer data and happens *before* any Noise session, so the coordinator never holds a
   Noise key. The relay must **reject probe headers arriving from agents** (only coordinator-originated), so an agent can't use it
   to scan a customer's network. `Relay.ProbeGateway(ctx, gatewayID, service)` is what the portal calls.
3. Check 1 needs no gateway change: relay session alive plus QUIC round-trip time.
4. UI: a "Test connection" button per gateway card. **Throttled in the frontend only**: disabled while a test runs, plus a ~10s
   cooldown. Result shown as the colored headline and a checklist with one line per check and a one-line fix for each failure
   ("connection refused: nothing is listening on that port on the gateway's machine").
5. **Legend:** a "?" icon beside the button opening a popover explaining the four states and what to do for each; colors come from
   the existing status registry (`ui/status.ts`).

**As built (differences from the sketch, and why):**
- **Probe protocol.** The relay opens a stream to the gateway whose "task id" is `probe:<service>` (`quic.ProbePrefix`); the gateway dials the named
  service locally (2s timeout) and answers a plaintext `{ok, error, ms}` before any Noise session. The relay **refuses the marker on a stream an
  agent opens** (stream error code 5), so only the coordinator can probe. The test for that guard failed to fail when the guard was removed
  (it checked the error but `Read` returns bytes and an error together); it now demands zero bytes and the explicit refusal code, and was
  re-checked by mutation.
- **Old gateways are told apart without ambiguity**: the coordinator waits 4s per service for a reply, longer than the gateway's own 2s dial
  timeout, so "no reply" can only mean the gateway doesn't speak the probe, never "the dial was slow". That is the grey state, and the
  response never reads green unless every service answered (`gatewayTestStatus`, a table-tested pure function).
- **It checks what is registered, not just what the gateway thinks**: the coordinator asks about each service in the portal's record, so a service
  listed in the portal but missing from the gateway's own `LAZYCAKE_SERVICES` is reported ("this gateway doesn't publish a service with that name").
- Endpoint `POST /api/portal/customer/gateways/{id}/test` -> `{status, connected, rtt_ms, verified, services[{name,port,ok,error,ms,answered}], tested_at_ms}`;
  6s overall timeout; 404 for someone else's gateway; 503 if the server has no prober. No server-side rate limit (decided).
- UI: a per-card "Test connection" button (disabled while running and for 10s after, frontend-only, with a countdown), the coloured headline, a row per
  service with a plain-language reason, "tested Xs ago", and a "?" legend popover whose wording shares one source (`gatewayTestStatuses`) with the result.
- **Requires a gateway update** for the per-service check. Gateways built before this show grey until updated.

**Verified:** Go tests over real QUIC and real TCP (all services up; one service refusing and one unpublished, reported separately; gateway not
connected; a gateway that disconnects mid-way reads as gone at once; an old gateway that never replies reads unverified; an agent can't probe), HTTP
tests of all four states plus ownership and the missing-prober case; and the real UI against real gateway processes: a new gateway with a live and a
dead service (yellow, "connection refused: nothing is listening on that port on the gateway's machine"), a new gateway with a live service (green),
the old v0.2.0 gateway (grey), three offline gateways (red); cooldown on every button; legend. `e2e/behaviour.mjs` covers the parts that need no
live gateway.

**Original verification list:** all-green; a service port with nothing listening (yellow); gateway stopped (red); an old gateway (grey, and
no green ever); two services where one fails; timeout path; an agent sending a probe header is rejected; the throttle (button
disabled during and after a run). Redeploy both of the owner's gateways (VM and server) and test against the real ones.

### Task 8.14 — Gateway traffic: totals, time series, and per-task/per-gateway usage

**Status: backlog, not started.** Suggested 2026-10-07.

**Today.** The gateway already reports bytes after each forwarded connection (`ReportBytes`: gateway ID, task ID, and the two
directions separately). The coordinator throws the gateway ID and the direction away: it adds the total into an in-memory per-task
counter used for billing reconciliation (`billing.Reconciler`), which also resets on restart. So nothing about per-gateway usage is
kept anywhere.

**Decision: Postgres, not InfluxDB.** The droplet has ~450 MB RAM and the coordinator runs under a 150 MB cap; a second datastore
means another service to deploy, secure and back up, against the project's "Postgres only, no broker" design; and the volume is one
row per forwarded connection (thousands a day). Revisit a time-series store (TimescaleDB first, since it is still Postgres) only if we
add live per-second metrics or CPU/memory series for tasks and nodes. The raw rows can be moved later.

**Build:**
1. Migration: an append-only `gateway_transfers` table, one row per forwarded connection (task, gateway, service, bytes each way,
   start and end). Retention: prune (or roll up to daily totals) after 60-90 days so it can't fill the 10 GB disk.
2. Ingest in `GatewayServer.ReportBytes` (it already receives everything needed); keep feeding the existing reconciler unchanged.
3. **Progress reports:** today a connection is reported only when it closes, so a 2-hour database session would appear as one spike at
   the end. The gateway sends a small delta every ~10s while a connection is open, which makes "over time" honest. (Shares the gateway
   release with 8.13.)
4. API: per-gateway totals and daily/hourly series; per-task breakdown by gateway (and service); a gateway's busiest tasks.
5. UI: lifetime totals on each gateway card; a gateway detail view with a 7/30-day chart (existing `BarChart`) and its busiest tasks;
   on the task page's **Network** card, each target shows what actually flowed, e.g. `postgres:5432 via production-postgres  ↑ 12.4 MB
   ↓ 310 MB`, with a total across gateways, ticking up live for a running task. Optional "Transferred" column on the tasks list (off by default).
6. Labels from the customer's side, not the ambiguous "in/out": "Received from tasks" and "Sent to tasks", with arrows.

**Caveats to keep in the UI copy:** counts are "as reported by your gateway" and are a floor (a gateway that can't reach the
coordinator loses that report); there is no backfill, totals start the day this ships; the system also has agent-side and relay-side
counts and already flags large three-way disagreement, so show the gateway's (customer-owned) number and mark a task where they diverge.

**Verify:** per-gateway attribution when one task uses three gateways (no leakage between them); a long-lived connection shows
steady growth, not one spike; coordinator restart loses nothing already stored; retention pruning; migration up/down; the chart and
per-task numbers against a task whose traffic is known exactly (e.g. fetch a file of a known size).

### Task 8.15 — Machine usage: CPU, memory, disk and network over time, with per-task breakdown

**Status: backlog, not started.** Suggested 2026-10-07 (the per-task hover added the same day). Order of the queued work: 8.12, 8.13,
8.14, 8.15. 8.14 and 8.15 share their groundwork (time-bucketed tables, a rollup and pruning job, the chart components): build that once.

**What to measure.**
- *Tasks' usage*: CPU, memory, disk used by what LazyCake runs, read from the task containers' cgroups. This is the number a provider cares about
  ("how much of my offered 8 cores is in use?").
- *Host headroom*: the machine's total CPU/memory, so the provider can see whether their own work squeezes tasks out. **Totals only**, nothing about the
  provider's processes.
- *Network*: tasks have no network except the tunnel, so the meaningful number is tunnel bytes in/out per task. Skip host-wide interface counters (they mostly
  measure the provider's own traffic).
- *Disk*: space used by the agent's data directory (image cache, task scratch) against what was offered.

**Transport.** Ride the existing 15s heartbeat: additive fields on `Heartbeat` in `agent.proto`: machine totals plus **one entry per running task** (capped,
e.g. 32). No new connection. Old agents keep working and the machine page says "update the agent to see usage".

**Storage: Postgres, not InfluxDB** (same reasoning as 8.14: ~450 MB droplet, "Postgres only" design, tiny volume: ~6,000 rows/machine/day, ~0.6 MB).
- Raw samples 48h; 5-minute rollups 30 days; hourly rollups 1 year (a pruning/rollup job).
- Per-task usage in 5-minute buckets (a one-hour task is ~12 rows), plus **one permanent summary row per task** (total core-seconds, peak memory, total
  tunnel bytes) so per-task numbers survive after the detailed samples are pruned.
- Revisit TimescaleDB (still Postgres) at roughly a thousand machines or when long-retention analytics are wanted.

**UI.**
- Machine page: 24h / 7d charts for CPU, memory, disk and tunnel traffic; gaps where the machine was offline; live gauges ("1.4 of 2 offered cores in use");
  a small sparkline on each machine card.
- **Hover on the chart**: a stacked chart, one color per task (the biggest few plus an "others" band), and a tooltip that shows who was using what at that moment:
  `14:32 · 1.4 of 2 cores — tsk_01M4…8939 0.9, tsk_01M4…D4AB 0.5`.
- **Hover on a row in the machine's tasks table**: that task's totals (core-seconds, peak memory, disk, tunnel bytes).

**Cautions.**
- **Display only.** The agent reports these and the host controls the agent, so a dishonest host could fake them. They must never feed billing or the trust
  score (consistent with the threat model in the README).
- Providers already see task IDs and images on their machine page, so per-task consumption adds no new exposure (how much a task used, not what it does).
- Follow-up that reuses this pipeline: a customer-facing per-task chart of actual usage against the limits (pairs well with the benchmark workload).

**Verify:** a known workload (the benchmark task at 0.5 cores / 128 MB) shows ~0.5 cores and a memory curve that levels off; per-task attribution with
two tasks on one machine (no leakage); an offline gap renders as a gap, not zeros; rollups match the raw averages; pruning keeps the per-task summary;
an old agent shows the "update" message; heartbeat size with the cap hit; migration up/down.

### Task 8.16 — Gateway (and agent) version indicator with a guided update

**Status: idea, to be discussed (suggested 2026-10-07); not scheduled.**

**Decision so far: do not build a button that pushes an update.** The gateway runs on the customer's machine and only connects out; it is the customer's own
security boundary. A coordinator-initiated code replacement would put the platform in charge of what runs inside customers' networks (a compromised coordinator
could push a bad binary to every gateway), and would need signed releases plus permission to replace the running program.

**Proposed instead:**
1. The gateway reports its version when it connects (the version stamp added in v0.2.0 makes this possible). Each gateway card shows it, with an **"Update available"**
   badge when it is older than the coordinator's own release (no external lookup). Old gateways send no version: show "version unknown" and the badge.
   The connect frame is fixed-order length-prefixed strings, so add the field in a backward-compatible way (an optional trailing field the coordinator tolerates missing).
2. An **"Update" button that opens guided instructions** for how the gateway was installed (new binary, or `podman pull` and restart), with a copy button, the current and new
   versions, and where to get the release.
3. The gateway test's grey state ("services not verified: update your gateway") links to the same instructions.
4. The same mechanism for provider agents: show the agent version and "Update available" on the machine page.

**Later, opt-in only:** a gateway that updates itself when the customer turns it on (checks for releases, verifies a signature, replaces itself). Needs a signed-release pipeline.

**Order:** after 8.15, or ahead of 8.14 if solving updates first matters more.
