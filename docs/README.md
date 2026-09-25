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
- **[`01-dashboard-portals/`](01-dashboard-portals/)** — phase 7 (planned,
  not yet built): separate, authenticated Provider and Customer web
  portals replacing today's single unauthenticated global dashboard.
  Start with `01-dashboard-portals/PLAN.md`.

The top-level `README.md` is the project's front door and stays at the
repo root (GitHub renders it there); it links back into this tree for
everything else.
