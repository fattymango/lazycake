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

**Status: queued, not started.** Requested 2026-10-07; to be built once the owner has verified the redesigned UI end
to end. Nothing in the product can stop a task on request today: the old UI never had a stop button, there is no
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

**Verify (these are the risky parts, so test them, don't just try it once):** cancel a queued task; cancel a running
task and check the container is really gone on the node; cancel a task that is finishing in the same instant (no double
settlement, no negative balance); cancel twice; cancel someone else's task (404); cancel while the node is offline;
the ledger and balance afterwards. Add the e2e check to `web/e2e/behaviour.mjs`.

### Task 8.13 — Gateway "Test connection" (with a legend)

**Status: backlog, not started.** Suggested 2026-10-07. Order of the queued work: 8.12 (stop a task), then 8.13, then
8.14. 8.13 and 8.14 both change the gateway binary, so they should ship in one gateway release.

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

**Verify:** all-green; a service port with nothing listening (yellow); gateway stopped (red); an old gateway (grey, and
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
