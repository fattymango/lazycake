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
