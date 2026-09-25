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
  authenticated Provider and Customer web portals replacing today's single
  unauthenticated global dashboard. The backend (`portalapi`, tasks
  7.1-7.5) is complete and verified against real Postgres; the frontend
  (`web/`, tasks 7.6-7.9) is code-complete but not yet exercised against
  it in a real browser; task 7.10 (relocate `/ops`, serve the built
  frontend) is not started. Start with `01-dashboard-portals/PLAN.md`,
  then `PROGRESS.md` for current state.

The top-level `README.md` is the project's front door and stays at the
repo root (GitHub renders it there); it links back into this tree for
everything else.
