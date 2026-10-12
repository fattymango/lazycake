# Changelog

Versions follow `vMAJOR.MINOR.PATCH`. Every binary reports its version (`coordinator --version`, `agent --version`, ...) and the
coordinator logs it at startup.

## v0.4.17 — 2026-10-12

Coordinator only; agents and gateways are unchanged. No migration.

### Changed
- **The Overview success rate covers the last 7 days** instead of the last 50 finished tasks: "45 of 50 this week". Running and queued counts are unchanged (all tasks).

## v0.4.16 — 2026-10-11

Coordinator only; agents and gateways are unchanged. No migration.

### Fixed
- **The big-task hold cost too much throughput.** v0.4.15 made every machine that could run a waiting big task stop taking new work until it was empty, so in the second load test machines sat mostly idle (3 tasks running with 270 queued) and 633 tasks finished against 855 before. Now only one machine empties itself per waiting task, and while it does, short tasks (wall-clock limit shorter than the wait) still run on it. Waiting times for 4-core tasks fell from 31 to 20 minutes median in that run and 8-core from 42 to 26.

## v0.4.15 — 2026-10-11

Coordinator and agent (agents show "Update available"; gateways are unchanged). No migration.

### Fixed
- **Big tasks no longer starve.** A task that has waited more than 90 seconds now holds back the machines that could run it: they stop taking newer work until it fits, then it goes first. In the load test 4-core tasks waited 31 minutes and 8-core 42 minutes behind a stream of small ones. A task a machine just rejected no longer blocks that machine.
- **Trust survives a coordinator restart.** Scores were saved but never read back, so every deploy reset all machines to 0.5.
- **Agents reconnect quickly after a coordinator restart.** The reconnect delay never reset, so agents waited about a minute, long enough to fence running tasks. It resets after a connection that lasted 30 seconds.
- **A task's tunnel survives a dropped relay connection.** The first dial is retried and a dead connection is replaced once, instead of failing the task or every stream.

### Added
- **Reserved cores and memory on machine cards and pages** ("Reserved by tasks: 8 of 8 cores"), next to the CPU in use, so a full machine no longer looks like it has spare room.

## v0.4.14 — 2026-10-11

Coordinator only; agents and gateways are unchanged and are not rebuilt or restarted.

### Fixed
- **A machine is filled faster and is no longer sent work it has no room for.** The scheduler learned a machine's free capacity only from the agent's next report, so it kept sending work to a machine it had just filled and the agent turned it away ("does not fit free capacity"): in the production load test about a quarter of all dispatches (29 of 107 in six minutes) were rejected and put back in the queue with a 30-second cool-down. It also gave a machine one task per second, so a machine that freed many cores at once refilled slowly. The scheduler now subtracts every task it dispatches from the machine's free capacity straight away (the agent's next report still corrects it) and keeps placing tasks on a machine within the same second for as long as they fit.

- **The Overview counted tasks from the newest 200 only.** With a long queue the "Running now", "In the queue" and "Finished" figures and the success ring were wrong: 200 queued tasks filled the list, so it read "0 running, nothing finished" and a queue of "200" however long it really was (288 queued, 12 running and 455 finished in the load test). The counts now come from the server over all of an account's tasks. The same fix applies on the Gateways pages: tasks using gateways now, a gateway's running and queued tasks, its last task, and whether it can be deleted.

- The load-test tool's report called "tasks created in the last 3 hours" `db_errors`; it is now `tasks_created_last_3h`. The run itself is written up in `docs/LOAD_TEST_2026-10-11.md`.

### Good to know (not a change)
- Cores are reserved by what a task asked for, not by how much CPU it uses at that moment, so a machine can be "full" for the scheduler while the CPU graphs show spare room: tasks that sleep, wait for the network or are mostly idle reserve more than they burn. In the load test the PC had 8 of 8 cores reserved while its tasks used 5.0, and the VM 3.5 of 4 reserved for 1.0 used.

## v0.4.13 — 2026-10-11

Coordinator only; agents and gateways are unchanged and are not rebuilt or restarted.

### Added
- **Live bandwidth for gateways.** The gateway page opens with a Live bandwidth card: how much the gateway is moving right now (both directions together, and each on its own), the busiest 10 seconds of the last five minutes, a gauge against the gateway's own limit when it has one, and a five-minute chart with the limit drawn as a dashed line. The Gateways page shows the total moving right now in its overview card and a "Right now" line on every gateway card. It refreshes every five seconds. The figures are the gateways' own byte counts, so they are exact, but they are averaged over the last 30 seconds and run about 10 to 15 seconds behind real time; the card says so. No change to the gateway or the agent: it reads the 10-second samples the coordinator already keeps.

## v0.4.12 — 2026-10-11

Needs migration 026 (before the coordinator) and an updated gateway. Agents are unchanged and are not rebuilt or restarted.

### Added
- **The dashboard shows each gateway's bandwidth limit.** The gateway now tells the coordinator the cap it enforces every time it connects, so the gateway card and the gateway page show what it is really running with: "50 Mbps", "No limit", or "Not reported" for a gateway older than this release (which is never shown as "No limit"). The limit is still set on the gateway's container; the dashboard only shows it. Migration 026 adds `gateways.max_mbps`.

### Changed
- **Agents and gateways now have separate "newest release" numbers for the Update badge.** Last release the badge stopped following the coordinator; it now also stops treating a change to one program as news for the other, so a gateway-only release does not mark every machine "Update available" (and the other way round). Each has its own test that fails until the number is bumped with its code.

## v0.4.11 — 2026-10-11

Coordinator only; agents and gateways are unchanged and are not rebuilt or restarted.

### Changed
- **"Update available" no longer depends on the coordinator's version.** The badge used to compare each agent and gateway with the coordinator's own release, so every dashboard or coordinator release marked all of them "outdated" even though nothing about them had changed. It now compares against the newest release in which the agent or gateway program itself changed (v0.4.4 today), so a machine or gateway on v0.4.4 or newer reads as up to date and the badges stay quiet through dashboard-only releases. A test fails whenever the agent or gateway code is edited without bumping that number, so a real agent change can't be released without the badge appearing.

## v0.4.10 — 2026-10-11

Dashboard only (the coordinator carries it); agents and gateways are unchanged, so they are not rebuilt or restarted for this release.

### Changed
- **The provider Overview gets the same treatment as the customer one.** The four tiles become two cards: lifetime earnings, the last 14 days and the earnings chart together (the chart moves up from the bottom), and a fleet card with a ring of how many machines are online, what is on offer right now and the average trust. The machines follow at full width.

### Added
- **The Gateways page opens with analytics.** A traffic card shows everything received from and sent to tasks through all gateways, with a bar per gateway ranking the busiest ones (only gateways that moved data are listed, the rest are summed up in one line); a status card shows a ring of how many gateways are connected, how many services they publish and how many unfinished tasks are using them right now.

## v0.4.9 — 2026-10-11

Dashboard only (the coordinator carries it); agents and gateways are unchanged, so they are not rebuilt or restarted for this release and will read "Update available" until the next release that changes them.

### Changed
- **The customer Overview's four tiles are two cards.** The balance, what was spent in the last 7 days and the spend chart are one card (the chart moves up from the bottom), and the success rate becomes a ring with the running, queued and finished counts beside it. The Gateways card at the bottom is gone (the Gateways page has them). The recent-tasks table follows directly.

## v0.4.8 — 2026-10-11

Dashboard only (the coordinator carries it); agents and gateways are unchanged apart from the version number.

### Changed
- **The Overview pages have no side column any more.** The stat tiles run across the top and the main list takes the full width: the recent-tasks table for customers (the compact list from the last release is gone), the machine cards in a grid for providers. The charts and gateways sit underneath: Spend and Gateways side by side, and the Earnings chart full width. Tasks, machines and gateways are the point of those pages, so they are never pushed into a narrow column.

## v0.4.7 — 2026-10-11

Dashboard only (the coordinator carries it); agents and gateways are unchanged apart from the version number.

### Changed
- **The two Overview pages now lead with the analytics.** The stat tiles run across the top of the big left column with a large chart under them (spend for customers, earnings for providers), and the lists move to the right-hand column: recent tasks and gateways for customers, the machines for providers. The recent tasks there are a compact list (id, state, image, age and charge) instead of the wide table; the full table is still on the Tasks page.
- **Fixed:** on a phone, a card whose header has a control next to its title (for example the task page's Resource use) squeezed the title to a few letters. The control now wraps under the title.

## v0.4.6 — 2026-10-11

Dashboard only (the coordinator carries it); agents and gateways are unchanged apart from the version number.

### Changed
- **The dashboard uses the whole screen.** Pages are no longer held to a narrow centred column: content now fills the window (up to 1792 px) instead of leaving about 265 px of empty space on each side of a 1920 px screen.
- **Pages are laid out side by side instead of one long column.** The main subject sits on the left and stats and secondary cards on the right: the recent-tasks table with balance, running, queue, success rate, spend and gateways beside it; the machine list with earnings and trust beside it; a machine's usage and tasks with its status, trust and capacity beside them; a gateway's traffic with its totals, targets and activity beside it; billing and earnings with the transactions on the left and the figures on the right. The new-task form puts "What to run" and "Resources" next to each other on large screens. Machine and gateway lists use as many columns as fit (four on a full-HD screen). On windows narrower than 1400 px, and on phones, the right-hand column drops below the main one, so nothing is cut off.

## v0.4.5 — 2026-10-11

Dashboard only (the coordinator carries it); agents and gateways are unchanged apart from the version number.

### Added
- **An "i" next to every trust score** (machine cards, the machine page and the Average trust tile) opens a plain explanation of what trust is: the 0.50 start, what raises and lowers it and by how much, that below 0.20 a machine gets no work, how it recovers, and that scores restart at 0.50 when the coordinator restarts. It opens by click, tap or keyboard, and does not trigger the machine card it sits in.

## v0.4.4 — 2026-10-11

Needs migration 025 (before the coordinator) and updated agents. Gateways are unchanged apart from the version number.

### Changed
- **A machine keeps one entry across agent restarts, updates and reboots (task 8.19).** The agent keeps a machine id in the host directory it already mounts (the one that survives recreating the container), and the coordinator keeps one machine entry per account and machine id. Updating an agent no longer leaves an offline duplicate behind, and the machine keeps its trust, so restarting an agent can no longer be used to shed a low score. The first time an updated agent registers, it takes over the newest offline entry of the same host name instead of adding a new one; older duplicate entries stay until removed by hand. An agent that cannot write the directory, or one older than this release, behaves as before (an entry per start). Two computers cloned from one disk image would share an id.
- **Tasks an agent lost in a restart are reclaimed at once.** Because the machine is now the same node, it never looks lost. When it registers as a new process, any task still assigned to it that it no longer runs is ended right away (abandoned for the default at-most-once delivery, requeued for at-least-once) instead of waiting for its lease to run out.

## v0.4.3 — 2026-10-11

Coordinator only: no migration, agents and gateways unchanged.

### Added
- **Rate limiting and abuse protection.** The portal limits each IP address: 10 login attempts a minute and 5 sign-ups per 10 minutes (answered `429` with `Retry-After`), and a generous 30 requests a second for everything else. This closes the gap where one address could try a password against thousands of usernames without ever tripping the per-username lockout. An address that fails authentication 20 times in a minute is blocked for five minutes on the agent/customer gRPC port and on the tunnel relay. JSON request bodies are capped at 1 MiB (`413`), and the HTTP server now times out slow or idle connections (slowloris). Limits are per IP, in memory, and reset when the coordinator restarts. This does not stop a volumetric DDoS: that needs a filter in front of the server.

## v0.4.2 — 2026-10-11

Needs migration 024 (before the coordinator) and an updated gateway image.

### Added
- **Delete a gateway** (trash button on the gateway card and its page). Allowed only while no queued or running task uses it (the button shows why when it can't be used). Deleting archives it (history stays, it leaves the list), expires its install token immediately, and drops its live connection. The gateway on the owner's machine stops serving and goes idle, and stays idle across container restarts; running it again by hand is refused by the coordinator. The dashboard then shows the command that removes the container and its key from the machine, since LazyCake has no access to it. Tasks can no longer target a deleted gateway. Migration 024 adds `gateways.archived_at` and `gateways.token_hash`; gateways created before it have no recorded token, so their token cannot be singled out, but they are refused by id all the same. Gateways older than this release do not stop by themselves; they just keep being refused.

## v0.4.1 — 2026-10-11

### Added
- **Gateway page shows what it targets and what is using it:** published services, status, version, last traffic, the newest task that used it, and running and queued tasks (with links). Derived in the browser from the account's recent tasks (the newest 200) and the gateway's traffic series; no API change.
- **Guided gateway setup:** creating a gateway now ends in four steps like the machine setup (check the machine, start the container, survive reboots, wait for it to connect, which turns green by itself).
- **Optional bandwidth limit**, chosen at creation and enforced by the gateway itself (`LAZYCAKE_MAX_MBPS`, whole Mbps, 0 or unset = unlimited): all tasks and both directions share it. It is a setting of the container, not stored by the coordinator, so the dashboard cannot show it afterwards; change it by recreating the container. Needs an updated gateway.

## v0.4.0 — 2026-10-11

The backlog from the production load test, and the version indicator (task 8.16). Needs migration 023, a coordinator deploy, and updated agents and gateways.

### Added
- **Every machine and gateway shows the version it runs, and updating is a guided copy-and-paste.** The agent sends its version when it registers; a gateway says hello with its version after it connects (an older gateway
  never does, so it shows "version unknown"). The dashboard compares it with this coordinator's own release and shows an **Update available** badge on the machine page, the machines list, the gateway cards and the
  gateway test's grey result. The badge opens a dialog with the current and the latest version and one command that pulls the new image and recreates the container with the settings it already has (restart policy
  included). Nothing is pushed to anyone's computer: the program runs on the owner's machine and stays under their control. Migration 023 adds `nodes.agent_version` and `gateways.version`.
- **The agent tells the coordinator how much it has free** (cores, memory, disk), on start, whenever it changes and every 30 seconds, and the scheduler only dispatches a task to a machine with room for it. In the load
  test 357 tasks were handed to a machine that had no room, refused by the agent, and each refusal cost a cooldown.

### Fixed
- **A task that never started no longer keeps the customer's money on hold.** A task that could not start (a nonexistent image, a refusal) placed its worst-case hold when it was dispatched and never released it: the
  coordinator logged "meter not found" and returned. The load test left 34 of these on customers' balances. The hold is now released when such a task finishes.
- **The report of a task that finished while the agent was reconnecting is no longer lost.** The agent kept its final reports to resend after a reconnect, but the connection was only ready to send after that resend ran, so
  the report was dropped and the coordinator went on believing the task was running. That includes the "fenced" report of a task the agent stopped itself after losing the coordinator. A report is now kept until a connection has
  taken it. Needs the updated agent.
- **An offer's disk size is checked against the disk's size, not against how much is free that day.** A machine that started fine yesterday could be refused today because a few MB of unrelated files appeared. The agent now
  warns when the offer is more than what is currently free.
- A repeated capacity report no longer rewrites the machine's offer in the database.

## v0.3.13 — 2026-10-11

### Fixed
- **An agent really does recover after its machine suspends.** v0.3.12 noticed the dead connection but then reconnected on the same dead socket: the kernel keeps unacknowledged data for about 15 minutes, and the agent's
  gRPC channel reused that socket for every new stream. The agent's connection now uses a 30-second TCP user timeout and keepalives, so the dead socket is torn down and replaced. Needs the updated agent.

## v0.3.12 — 2026-10-08

### Fixed
- **An agent recovers by itself after its machine suspends.** When a laptop slept and woke, the connection to the coordinator was dead but gave no error, so the agent kept sending heartbeats into it, fenced its tasks, and
  then waited forever for answers that would never come. The coordinator showed the machine as down until the container was restarted by hand. The agent now treats three heartbeat intervals without an acknowledgement as a
  dead connection and reconnects. Needs the updated agent (`docker.io/fattymango/lazycake-agent:v0.3.12`).

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
