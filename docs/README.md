# Documentation index

Organized by phase/position in the project's build order — oldest first.

- **[`00-core-platform/`](00-core-platform/)** — phases 0–6: the dispatch
  engine, tunnel, leases/fencing, metering, trust, and the original
  dashboard/demo. Fully implemented and running live. Start with
  `00-core-platform/PLAN.md` for the design rationale, then
  `00-core-platform/IMPLEMENTATION.md` for the task-by-task build order.
  `00-core-platform/PROGRESS.md` has a dated entry for every task;
  `00-core-platform/OPEN_QUESTIONS.md` has every environment limitation
  and judgment call encountered building it.
- **[`01-dashboard-portals/`](01-dashboard-portals/)** — phase 7: separate,
  authenticated Provider and Customer web portals (username/password
  sessions, account-scoped API, embedded in the coordinator binary). Built,
  deployed and in use. Start with `01-dashboard-portals/PLAN.md`, then
  `PROGRESS.md` for the history of what was verified live.
- **[`02-frontend-overhaul/`](02-frontend-overhaul/)** — phase 8: a
  professional design system and a redesign of both portals, plus the fixes
  for the refresh-redirect-loop and repeating-log bugs. Start with
  `02-frontend-overhaul/PLAN.md`; `PROGRESS.md` has the dated log and what
  was verified in a real browser. Day-to-day frontend guidance lives in
  `web/README.md`.

The top-level `README.md` is the project's front door and stays at the
repo root (GitHub renders it there); it links back into this tree for
everything else.
