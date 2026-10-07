# Changelog

Versions follow `vMAJOR.MINOR.PATCH`. Every binary reports its version (`coordinator --version`, `agent --version`, ...) and the
coordinator logs it at startup.

## Unreleased

### Added
- **Stop a task** (task 8.12): a "Stop task" button on the task page, `lcctl cancel <id>`, `POST /api/portal/customer/tasks/{id}/cancel` and the
  `CancelTask` RPC. A queued task is cancelled at once and costs nothing; a running one is stopped on its node and you are charged only for the time
  it ran. Shown as "Stopping" until the node confirms, then "Stopped by you". Needs migration 016.
- **Gateway "Test connection"** (task 8.13): checks that a gateway is connected and that each service it publishes actually answers on the gateway's own machine.
  Green (all reachable), yellow (connected but a service isn't), red (not connected), grey (connected, but the gateway is too old to verify services, so update it),
  with a legend and per-service reasons. The per-service check needs the new gateway build. Throttled in the UI only.
- **Gateway traffic** (task 8.14): every gateway card shows how much it has moved ("Received from tasks" / "Sent to tasks"), each gateway has a page with a 7- or
  30-day chart and its busiest tasks, and a task's Network card shows what it moved through each service. A gateway reports every 10 seconds while a connection
  is open, so a long database session shows up as it happens. Needs migration 017; update the gateway for the over-time numbers (older gateways still count,
  once per connection).
- **Machine usage** (task 8.15): the machine page shows what the machine's tasks are using right now (CPU, memory, disk against what's on offer) and a 24-hour or 7-day
  history; hovering the chart lists which task used what, offline periods show as gaps, and hovering a task row shows its CPU time, peak memory and tunnel traffic. Machine cards
  get a CPU trend. Display only: it never affects billing or trust. Needs migration 018 and an updated agent.

## v0.2.0 — 2026-10-07

The web portals, a redesigned UI, public images, and a round of fixes found by running the whole thing for real.

### Added
- **Customer and provider web portals**, served by the coordinator itself: username/password sessions, account-scoped API, live
  updates over a single shared event stream. Customers submit tasks (with templates), watch live logs, manage gateways and billing;
  providers register machines with a guided flow that detects the connection, and track earnings.
- **A design system and full UI redesign** (phase 8): dark and light themes, app shell, data tables that can't overflow, shortened
  copyable IDs, a terminal-style log viewer (search, stderr filter, wrap, copy, download), charts, designed empty/loading/error/404
  states. See `web/README.md` and `docs/02-frontend-overhaul/`.
- **Public images on Docker Hub:** `fattymango/lazycake-agent` and `fattymango/lcbench` (a benchmark workload that prints its cgroup CPU
  and memory usage next to its limits).
- **Gateways reconnect on their own** (capped backoff) instead of exiting when the relay drops; the relay tells peers when it shuts down.
- Task view API now returns `wall_timeout_s`, `tunnel_targets` and `attempt`.
- Version stamping in every binary; `make web-check`, `make web-embed`.
- Browser test tooling (`web/e2e`): screenshots + overflow audit across widths/themes, and behavioural checks run against the production build.

### Fixed
- **Refreshing a deep link (e.g. an open task) landed on Chrome's "too many redirects" page**: the SPA fallback redirect-looped through Go's file server.
- **A finished task's log repeated forever**: the stream now carries ids, honours `Last-Event-ID`, and ends with an explicit `end` event.
- **Live streams leaked across full-page navigations** until requests stalled at the browser's 6-connection limit.
- Signing in as a different role after logout sent you to the previous account's page.
- Costs and spend showed as negative (the ledger stores charges as negative amounts).
- A coordinator restart could hang for 90s on an open agent stream; stale gateway `connected` flags survived a restart; a reconnecting gateway could be marked disconnected by its old connection's cleanup.
- A data race in the agent's `Runner.Send` (found by `-race`).
- The provider install command pulled a bare image name and the agent then failed every task (its `lcinit` path was resolved inside the agent's own container); both fixed (`LAZYCAKE_LCINIT_HOST_DIR`).
- The Add machine page's podman check failed on current podman (`CgroupsVersion`).
- Unknown `/api/portal/*` paths return the API's JSON error shape.

### Known limitations / next
- Gateways can't be tested from the UI; gateway traffic and machine usage aren't recorded over time.
  Planned as tasks 8.12–8.15 in `docs/02-frontend-overhaul/IMPLEMENTATION.md`.
- Two coordinators sharing one database fight over node `connected` flags; run a single coordinator.

## v0.1.0

Initial implementation of phases 0–6: dispatch engine, QUIC/Noise tunnel and gateways, leases and fencing, metering and billing, trust scoring,
the original operator dashboard and chaos demo.
