# LazyCake web

The customer and provider portals: React + TypeScript + Vite + Tailwind (v3), built into
`internal/coordinator/webassets/dist` and embedded in the coordinator binary, so there is nothing to deploy
separately. Design rationale and history: `docs/02-frontend-overhaul/`.

## Develop

```sh
npm ci
LAZYCAKE_PROXY=http://127.0.0.1:8080 npm run dev     # proxies /api/portal to a running coordinator
npm run build          # tsc + vite build
npm test               # vitest (unit tests for the logic that has to be right)
npm run lint && npm run format:check
```

Browser verification (needs Chrome and a coordinator with the demo data; see the header of each script):

```sh
python3 e2e/seed.py | psql "$LAZYCAKE_DATABASE_URL"      # realistic demo data; destructive, demo databases only
BASE=http://127.0.0.1:5173 node e2e/shoot.mjs --out /tmp/shots   # every page x 3 widths x 2 themes + overflow audit
BASE=http://127.0.0.1:8080 node e2e/behaviour.mjs                # deep-link refresh + log-stream regression checks
```

`behaviour.mjs` must run against the **production build served by the coordinator** (`make web-embed`), not the
Vite dev server: the refresh bug lived in the Go file server.

## Layout

```
src/
  ui/          the design system: tokens, primitives, shell. No product knowledge.
  components/  product components shared between portals (TaskTable, LogViewer, TrustMeter)
  features/    one folder per area: auth/, customer/, provider/, each with its own routes
  lib/         API client, auth, theme, live-event bus, formatting, hooks
```

## Rules that keep it consistent

- **No raw colors in components.** Every color is a token in `ui/tokens.css`, mapped in `tailwind.config.js`
  (`bg-surface`, `text-muted`, `border-border`, `text-accent`...). A rebrand is a change to that one file.
- **Long identifiers never overflow.** Show IDs with `<Identifier>` (shortens in the middle, full value on hover, one-click
  copy) and other single-line text with `<Truncate>`. Tables are `table-fixed` (`DataTable`), so a cell can't stretch them.
  `e2e/shoot.mjs` fails the run if anything pokes outside its container.
- **One live-event stream per tab.** Pages call `useLiveEvents` / `useLiveReload`; they never open their own `EventSource`
  (browsers allow 6 connections per host). Streaming hooks close on `pagehide` (see `usePageRestore`).
- **Every list has loading, empty and error states.** Use `DataTable`'s `loading`/`empty`, `EmptyState`, `ErrorState`.
- **Numbers go through `lib/format.ts`** so units and precision match everywhere.

## How to extend

**Add a page.** Create `features/<area>/MyPage.tsx` (start with `<PageHeader>` and `<Card>`s), add a `<Route>` in that
area's `*App.tsx`, and a nav entry (`nav`) and breadcrumb label (`crumbs`) in the same file.

**Add a task/node status.** One entry in `ui/status.ts` (`taskStatus`): label, tone, icon, description. Pills, the
overview counts and the timeline pick it up.

**Add a task template.** One entry in `features/customer/presets.ts`. Images must be pinned by digest.

**Add a theme.** Add a `[data-theme="name"]` block to `ui/tokens.css` and list the name in `lib/theme.tsx` and
`ui/ThemeToggle.tsx`.

**Add a primitive.** Put it in `ui/`, build it from tokens, make it keyboard- and screen-reader-friendly (Radix for
dialogs, menus, tooltips), and add it to the dev-only gallery at `/_kit` (`features/kit/Kit.tsx`) with awkward content.
