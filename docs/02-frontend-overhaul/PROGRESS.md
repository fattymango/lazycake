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
- Tasks 8.3–8.9 written, 2026-10-06, NOT yet fully verified (work in progress, uncommitted until now):
  - New design system under `web/src/ui` (tokens, themes, primitives, shell, DataTable, Identifier/Truncate,
    LogViewer, charts), data layer in `web/src/lib`, pages in `web/src/features/{auth,customer,provider}`.
    Old `shared/`, `customer/`, `provider/`, `pages/` deleted. One shared SSE stream per session
    (`lib/live.tsx`); the log hook de-duplicates by `seq` and closes on `end`.
  - Backend: task view now also returns `wall_timeout_s`, `tunnel_targets`, `attempt` (additive).
  - Tooling: Prettier, Vitest (21 unit tests pass), puppeteer-core e2e (`web/e2e/shoot.mjs` screenshots +
    overflow audit, `web/e2e/behaviour.mjs` deep-link/refresh and log-repeat checks, `web/e2e/seed.py` demo data).
  - Eyeballed in dark theme at 1440px: login, kit, customer overview/tasks/task detail/submit/gateways/billing,
    provider overview/machine/add. `tsc` and unit tests are clean.
  - NOT done yet: run `shoot.mjs` full audit (3 widths x 2 themes) and fix what it flags; run `behaviour.mjs`
    against the production build (build with `vite build --outDir ../internal/coordinator/webassets/dist
    --emptyOutDir`, then run the Go coordinator on :18081); light-theme and phone-width review of the new pages;
    ESLint pass; `docs/screenshots`, README and `web/README.md` (task 8.10/8.11); deploy to the server.
