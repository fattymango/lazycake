# Changelog

Versions follow `vMAJOR.MINOR.PATCH`. Every binary reports its version (`coordinator --version`, `agent --version`, ...) and the
coordinator logs it at startup.

## v0.3.11 — 2026-10-07

### Fixed
- **The agent no longer contacts the registry for an image it already has.** Task images are pinned by digest, so a cached copy is exactly the image asked for, yet the agent pulled before every task. A few hundred
  tasks used up Docker Hub's anonymous pull allowance for the machine's address, after which it could not start tasks at all, even with every image cached, and the failed pulls kept its capacity reserved. Found by a
  production load test. Needs the updated agent (`docker.io/fattymango/lazycake-agent:v0.3.11`).

## v0.3.10 — 2026-10-07

Another finding from the production load test: network-heavy tasks could take the coordinator down far enough that a machine fenced its own tasks.

### Fixed
- **A gateway batches its byte reports.** It used to send one report to the coordinator for every connection a task opened, and each was a database transaction. A task making hundreds of tiny connections a second
  starved the coordinator's heartbeats, a machine's lease expired, it fenced 10 tasks, and the penalty banned it. Now the gateway sends one report per task and service every 5 seconds, with exact totals and a count of
  how many connections closed. **Update your gateways** (`docker.io/fattymango/lazycake-gateway`); an older gateway still works, one report per connection as before.
- **The agent comes back after a reboot.** The install command now has `--restart=always` (and the page says to enable `podman-restart.service`). Before, a reboot left the agent container exited and the machine silently out of the pool.
  Machines already installed need the command run again.

## v0.3.9 — 2026-10-07

### Fixed
- **Spec drift ignores tasks too short to measure.** A second load test banned the PC again: six sub-second "hello" tasks (0.02 s to 0.3 s, all container start-up jitter) looked like a 10x slowdown. Tasks under 10 normalised
  seconds are now ignored for drift (they neither set a baseline nor flag).

## v0.3.8 — 2026-10-07

Found by a production load test: two connected machines were banned within three minutes of ordinary work, and nothing was dispatched to them afterwards.

### Fixed
- **Honest machines are no longer banned for running a mix of tasks.** Spec drift compared each task with the average of the machine's first five tasks, so a 90-second benchmark after a few instant tasks looked
  like a machine lying about its speed. It now compares a task only with the same workload (same image, command, environment and size) on the same machine, and only for tasks that exited normally.
- **The relay's encryption overhead is no longer blamed on the agent.** A task making hundreds of tiny connections showed a relay byte count 34% above the real traffic and was flagged as byte fraud. Only the agent and the
  gateway are held to agree (and only past 16 KiB); the relay can now only prove a claim too high.
- **A ban is no longer forever.** A banned machine got no tasks, so it could never earn trust back. Trust now heals with time (0.2 per hour) back to the starting 0.5, never above it.
- **A ban is no longer silent.** The scheduler logs when it stops and when it resumes dispatching to a machine. Before, 600 queued tasks and an empty log looked like a broken scheduler.

## v0.3.7 — 2026-10-07

### Changed
- The wide layout used by Add a machine tops out at 1400px (it was 1600px, which was too much on a big screen).

## v0.3.6 — 2026-10-07

### Changed
- **Add a machine uses the whole screen**: pages are held to a readable width by default, but this page is mostly controls side by side, so it now opts into a wider layout (`useWideLayout`, up to 1400px) instead of leaving empty margins on a big monitor.

## v0.3.5 — 2026-10-07

### Changed
- **Add a machine is more compact**: the hint sits beside each field's name instead of under the slider, the hardware check is tucked into a collapsible row, and "How your machine is protected" is a slim strip at the bottom.
- **The slider ends exactly at what your machine has** once you've checked its hardware (for example 1, 2, 4, 8, 12 on a 12-core machine), so you can pick the full amount.

## v0.3.4 — 2026-10-07

### Changed
- **Add a machine layout**: the capacity section is wider than the steps beside it, and the four resources sit in a 2×2 grid of tiles instead of a tall stack.

## v0.3.3 — 2026-10-07

### Changed
- **A redesigned "Add a machine" page**: what to lend on the left, the generated install steps beside it. Whole numbers only, a slider that snaps to checkpoints (CPU up to 24 cores, memory 64 GB, storage 100 GB)
  with a number field that can go past the slider, overflow-safe input, a warning that a machine that can't provide the numbers will fail, and a warning when you offer a lot of your own machine.

## v0.3.2 — 2026-10-07

### Added
- **Choose what to lend when adding a machine**: CPU, memory, storage and network, with the limits following what the machine really has (run `agent capacity` on it and paste the line). The install
  command carries your choice. Network is a new offer and is enforced: all task traffic through the machine shares the bandwidth you set.

### Changed
- **The agent rejects an offer bigger than the machine instead of silently shrinking it.** It refuses to start and says which setting to lower. The old built-in headroom (75% of cores, 2 GB RAM, 10 GB disk)
  is gone: leaving some for yourself is now your choice. Needs migration 022 and the updated agent.

## v0.3.1 — 2026-10-07

### Changed
- **A gateway always runs as a container.** There is now a public image, `docker.io/fattymango/lazycake-gateway`, and the dashboard's "New gateway" command is a `podman run` (host networking so it
  reaches your services, a volume for its key, restart on boot) instead of a binary to download. Existing gateways can be moved by running the new command; the old binary still works.

## v0.3.0 — 2026-10-07

Gateway traffic, machine usage, and a Resource use chart on every task. **Update your agents and gateways**: usage, per-task network and the trust fix only work with the new binaries.

Upgrade notes: the coordinator needs migrations 017, 018 and 019; the agent image is `docker.io/fattymango/lazycake-agent:v0.3.0` (also `:latest`).

### Changed
- **Charts are line charts**, one shared component everywhere; a 1-hour view for machines and 10-second gateway detail for tasks so short tasks draw a real line. Needs migration 020.

### Fixed
- **Trust no longer drops for machines that run tunnel tasks**: the agent didn't report its byte counts at task end, so every task that used a gateway looked like a 100% byte
  mismatch. It now reports them (needs the updated agent).

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
- **Network over time and a Resource use chart on the task page**: the machine page gets a Network tab (tunnel traffic per task over time), and every task page shows what the task
  actually used (CPU and memory against what it asked for, tunnel traffic in both directions, and its gateway traffic). Needs migration 019 and an updated agent.

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
