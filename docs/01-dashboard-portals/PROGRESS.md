# Phase 7 (Provider/Customer Portals) — Progress

Same convention as `docs/00-core-platform/PROGRESS.md`: one dated entry per
task, written honestly about what's actually done versus deferred.

- **7.6-7.9 (frontend, ahead of 7A) — Scaffolding, shared components, auth
  pages, both portals' pages.** Explicit project-owner direction: build
  Phase 7B (frontend) only, before Phase 7A (backend, tasks 7.1-7.5) exists,
  despite `IMPLEMENTATION.md`'s own stated order ("7A must land before 7B
  can call real endpoints"). Nothing here has been run against a live
  backend — the user will build/wire 7A and test locally.

  `web/` is a Vite + React + TypeScript + Tailwind project with three HTML
  entry points (`index.html` the two-link chooser, `customer.html`,
  `provider.html`), sharing `src/shared/`: `api.ts` (fetch wrapper, sends
  the session cookie via `credentials: "include"`, throws `ApiError` on
  non-2xx), `auth.tsx` (`useCustomerAuth`/`useProviderAuth` + `RequireAuth`
  route guard), `hooks/useSSE.ts`, and components (`NavShell`, `StatTile`,
  `DataTable`, `StateBadge`/`ConnectedBadge` — reusing the exact
  `state-*` → color mapping from
  `internal/coordinator/dashboard/index.html` per `PLAN.md` §6 — `Panel`,
  `Form` inputs, `LogViewer`, `AuthForm`, loading/error/empty states).

  Customer pages: `Dashboard`, `SubmitTask`, `TaskHistory`, `TaskDetail`
  (live logs + live state over SSE), `Gateways`, `Billing` (ledger + dev-only
  add-funds). Provider pages: `Dashboard`, `AddMachine` (mints an install
  token, shows the copy-pasteable install command, warns it's shown once),
  `MachineDetail` (capacity/trust + recent tasks), `Earnings`.

  `internal/coordinator/webassets` go:embeds `web/`'s build output
  (`dist/`, checked in as an empty placeholder — `dist/.gitkeep` — so
  `go build ./...` keeps working with no frontend build present).
  `deploy/Dockerfile` gained a `node:22-alpine` `web-build` stage that the
  `build` stage copies `dist/` from before compiling, so the single-binary
  deploy story is unchanged. Verified: `go build ./...` and
  `go vet ./internal/coordinator/webassets/...` both pass with the repo as
  it stands (no `portalapi` package yet, so nothing imports it from
  `cmd/coordinator`).

  **Not done, deliberately, because 7A doesn't exist yet:**
  - No task 7.10 backend changes: `internal/coordinator/dashboard` was left
    exactly as-is (still mounted at `/`, not `/ops`), and nothing wires
    `webassets`/a future `portalapi` into `cmd/coordinator/run.go`. Moving
    the existing dashboard and mounting the new one is real routing surgery
    that belongs with 7A's server wiring, not something to bolt on
    speculatively against a backend that doesn't exist.
  - `web/`'s `npm run build`/`podman build --target coordinator` verify
    steps in task 7.6 have not been run (no network access to npm's
    registry in this session — see OPEN_QUESTIONS.md).
  - Every page's `Done when` in `IMPLEMENTATION.md` requires a real running
    coordinator; none of that has been exercised. The API contract the
    frontend calls (`web/src/shared/types.ts`, `api.ts`) is the frontend's
    best-effort reading of `PLAN.md`'s Appendix and `store/types.go`, not a
    contract 7A has actually confirmed — check `types.ts`'s header comment
    and OPEN_QUESTIONS.md's "API surface the frontend assumes" section
    first when wiring up the real backend, since a few shapes (e.g. a
    per-node task list) had to be inferred rather than copied.

**Phase 7A (backend) not started.** Phase 7B (frontend) tasks 7.6-7.9 are
code-complete but unverified against a real backend; task 7.10 (retire
landing page, relocate `/ops`) is not started.
