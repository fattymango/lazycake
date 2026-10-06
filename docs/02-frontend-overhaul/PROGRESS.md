# Phase 8 — Progress

Dated entry per task, newest last. Include every bug found and how each was
verified.

- Phase planned, 2026-10-06 — owner called the Phase 7 UI "cheap and dated"
  and reported three problems: task IDs overflowing their fields, a failed
  task's log repeating forever, and a refreshed deep link (e.g. an open task)
  landing on Chrome's error page instead of the app. Root causes for the last
  two were found and confirmed before any code changed (see `PLAN.md` §2):
  `webassets.Handler`'s fallback redirect-loops through `http.FileServer`
  (reproduced against the live server: 10 redirects, still 301), and the log
  SSE stream closes on completion so the browser's EventSource reconnects and
  the server replays from seq 0. Existing frontend surveyed: 2.5k lines of
  React/TS/Tailwind v3 across `web/src/{customer,provider,pages,shared}`,
  hand-rolled icons and components, one-off colors in `tailwind.config.js`,
  no design tokens. Headless Chrome is available on the dev machine
  (`/usr/bin/google-chrome`) for screenshot verification.

- Tasks 8.1–8.2 done, 2026-10-06 (committed earlier as `8.1+8.2`): SPA fallback no longer
  redirect-loops; the log stream sends `id:<seq>`, honours `Last-Event-ID` and ends with an `end` event.
  Go tests fail without the fixes and pass with them.
- Tasks 8.3–8.11 done, 2026-10-06 (verified in a real browser against the production build):
  - Design system under `web/src/ui` (tokens + dark/light themes, primitives, app shell, DataTable, Identifier/Truncate,
    charts), product components in `web/src/components`, pages in `web/src/features/{auth,customer,provider}`,
    data layer in `web/src/lib`. The old `shared/`, `customer/`, `provider/`, `pages/` trees are deleted.
    `web/README.md` documents the rules and the extension points (page, status, template, theme, primitive).
  - Overflow: IDs shorten in the middle with full value on hover and one-click copy; hostnames/images truncate; tables are
    fixed-layout. `e2e/shoot.mjs` audited every page x {390, 820, 1440}px x {dark, light} (90 views): no layout problems.
  - Bug 1 (refresh -> Chrome error page): `e2e/behaviour.mjs` against the production build: deep links and refreshes return
    200 with no redirects; unknown routes show the app's 404; a missing asset is a real 404; an unknown API path is JSON.
  - Bug 2 (repeating log): a failed task's 17 lines stay 17 after 11s with exactly one stream request; a running task stays
    live with unique ascending line numbers. 19/19 checks pass.
  - Found while testing, fixed: full-page navigations left old documents parked in the back/forward cache with their
    EventSources open, until new requests stalled at the browser's 6-connection limit. Streaming hooks now close on
    `pagehide` and reconnect on a restore (`lib/hooks/usePageRestore.ts`). Unknown `/api/portal/*` paths now answer
    in the API's JSON error shape (was Go's plain-text 404), with a Go test.
  - Also: the shell opens one shared account event stream per tab (the old app opened up to three per page); the task view
    API gained `wall_timeout_s`, `tunnel_targets`, `attempt`; submit parses commands with quotes (`sh -c "a; b"`).
  - Checks: `make web-check` (tsc, ESLint, Prettier, 21 Vitest tests), `go vet`, and `go test -p 1 -race -tags=integration`
    (28 packages) all green. Screenshots are in `docs/screenshots/`.
  - Not done / follow-ups: no command palette, notifications centre or per-task resource charts (needs a metrics endpoint);
    the dev-only gallery at `/_kit` is the place to add new primitives; tablet width (820px) was audited for overflow
    but only dark desktop, light desktop and phone were reviewed by eye.

- Queued 2026-10-07: task 8.12, "Stop a task" (customer-initiated cancel). The owner asked whether the redesign had dropped a
  stop button; it hadn't: the product never had one (no endpoint, RPC or CLI command). Parked until the redesigned UI is
  signed off. Design notes and the verification list are in `IMPLEMENTATION.md`.

- Backlog 2026-10-07: tasks 8.13 (gateway "Test connection" with a green/yellow/red/grey result and a legend) and 8.14 (gateway
  traffic: Postgres event table, progress reports, per-gateway series, per-task per-gateway usage) designed and recorded in
  `IMPLEMENTATION.md`; InfluxDB considered and rejected for now (see 8.14 for why). Queue order: 8.12, 8.13, 8.14. Nothing built.

- Two bugs from owner testing, 2026-10-07, both fixed and deployed:
  - After signing out and signing in as a *different role*, the app sent the user to the previous account's page
    (`/tasks/<id>`, which doesn't exist for a provider). The route guard remembered the page for every sign-out. Now an explicit
    logout remembers nothing, and a remembered page is only honoured if the same role signs back in (and never off-site or back
    to /login): `lib/returnPath.ts`, 5 unit tests, plus a browser check in `e2e/behaviour.mjs` that reproduces the report (22/22 pass).
  - The "check the machine is ready" command on the Add machine page failed on the owner's podman (`can't evaluate field
    CgroupVersion`): the field is `CgroupsVersion`, and it differs across podman versions. The page now greps the stable JSON
    keys instead (`podman info --format json | grep -E '"(cgroupVersion|rootless)"'`), tested on podman 5.7; the same wrong
    name in `HANDOVER.md` is corrected.

- Owner feedback, 2026-10-07: (1) the dark theme was too dark: backgrounds, panels, borders and muted text lifted one small step
  (`--bg` 9 11 16 -> 17 20 29, etc. in `ui/tokens.css`), contrast unchanged in practice. (2) "Add a machine" never showed connected: the
  agent's log showed it dialing 127.0.0.1 (the local demo coordinator advertised a loopback address, which inside the agent's
  container is the container itself), and a rootless container also can't reach its own host's LAN address, so the local demo can only
  connect an agent with `--network=host`. The page now warns when the command points at a loopback address and, after 40s of
  waiting, lists what to check (`podman logs lazycake-agent`, ports 7443/tcp and 7444/udp, the address, a second run replacing the
  first). Verified in a real browser against a real agent: the page flips to "Your machine is connected". Not a product bug, but
  note: two coordinators pointed at one database fight over node `connected` flags (seen while testing), so don't run replicas
  against a shared database without addressing that.

- Backlog 2026-10-07: task 8.15 (machine usage over time with a per-task breakdown on hover) designed and recorded in `IMPLEMENTATION.md`; shares its
  time-series groundwork with 8.14. Queue order: 8.12, 8.13, 8.14, 8.15. Nothing built.

- Task 8.12 (stop a task) done, 2026-10-07: see "As built" in `IMPLEMENTATION.md`. New migration 016 (`tasks.cancel_requested_at`), a billable
  `stopped` exit reason, scheduler resend + dead-node handling, portal endpoint, gRPC `CancelTask`, `lcctl cancel`, and the Stop button.
  28 Go packages green with `-race`, including a real-container end-to-end test; 31/31 browser checks (9 new) and 37 unit tests. Found while
  testing: the e2e harness never wired the agent's cancel handler (production does), so it was added there; restarting a coordinator that
  has an agent connected takes up to 10s to release its port (the bounded graceful stop), which bit a local restart.

- Task 8.13 (gateway "Test connection") done, 2026-10-07: see "As built" in `IMPLEMENTATION.md`. Probe protocol in `internal/tunnel/quic/probe.go` and the gateway listener,
  `POST .../gateways/{id}/test`, the four-state result with a legend. Verified over real QUIC/TCP, over HTTP, and in a real browser against real gateway processes
  in all four states (36/36 browser checks). A self-inflicted slip caught on the way: a security test that couldn't fail (see the 8.13 notes). Needs a gateway
  update to get the per-service check; older gateways show grey.
