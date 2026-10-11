# Web frontend (`web/`)

React 18 + TypeScript + Vite + Tailwind 3 + Radix primitives + Vitest. Two portals (customer, provider) in one SPA, served by the coordinator from `internal/coordinator/webassets/dist` (gitignored; rebuilt before each coordinator build). Back to [README](README.md). API it talks to: [portal-api](portal-api.md). The in-repo guide is `web/README.md`; design history is `docs/02-frontend-overhaul/`.

## Layout
```
web/src/ui/          design system: tokens.css (all colors), primitives (Button, Card, DataTable, Dialog, LineChart, StopSlider, UpdateNotice…), AppShell, status.ts (state→label/tone)
web/src/components/  shared product components (TaskTable, LogViewer, TrustMeter)
web/src/features/    auth/, customer/ (Overview, Tasks, TaskDetail, SubmitTask, Gateways, GatewayDetail, GatewayTest, Billing…), provider/ (Overview, AddMachine, MachineDetail, MachineUsage, Earnings)
web/src/lib/         api.ts, auth, theme, live.tsx (SSE bus), format.ts, offer.ts, update.ts, usage/series/taskusage.ts (chart data shaping), types.ts
web/e2e/             puppeteer-core browser checks (*.mjs) + seed.py demo data
```

## Rules (enforced by review and e2e)
- No raw colors; use tokens. Long identifiers go through `<Identifier>`/`<Truncate>`; tables are `table-fixed`. `e2e/shoot.mjs` fails on overflow.
- One SSE stream per tab: use `useLiveEvents`/`useLiveReload`, never your own `EventSource`.
- Every list has loading, empty and error states. Numbers through `lib/format.ts`.
- **Measured values shown exactly** (e.g. CPU like 0.0017 cores); charts have hatched gaps for missing data, never fake zeros.
- Add-machine page: whole numbers only; sliders with checkpoints (CPU 1/2/4/8/16/24, memory 1–64 GB, storage 5–100 GB, network 10–1000 Mbps); the number field may exceed the slider (hard ceilings guard overflow); warnings when the offer is big or the machine might not provide it. Logic in `lib/offer.ts` (+ tests).
- Update flow (`lib/update.ts`, `ui/UpdateNotice.tsx`): `updateCommand(kind)` generates `podman pull <image>:latest` + recreate-from-recorded-command (removes the old container *after* reading the command). Changing it means updating `CLAUDE.md` §5 and its test.
- Deep-link refresh must work (SPA fallback in Go).

## Commands
`cd web && npm ci`; `npm run dev` (set `LAZYCAKE_PROXY`); `npm run build`; `npm test`; `make web-check` from the repo root (build + eslint + prettier + vitest); `make web-embed` builds into the Go embed dir. Node is not on PATH here: `export PATH="$HOME/.nvm/versions/node/v24.21.0/bin:$PATH"`. Browser scripts: `BASE=… SHOTS=/tmp/shots node e2e/<name>.mjs` (Chrome at `/usr/bin/google-chrome`; run `behaviour.mjs` against the embedded production build).

## Caveats
- `usage.mjs`/`taskusage.mjs`/`traffic.mjs` need real readings from a live agent/gateway; on an empty demo DB some checks fail for lack of data.
- Prettier formatting is checked in CI-style (`format:check`): run `npx prettier --write` on files you touch.
- Production serves plain HTTP; clipboard APIs may need a secure-context fallback (see `lib/clipboard.ts`).
