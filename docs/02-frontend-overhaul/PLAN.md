# LazyCake Phase 8 — Frontend Overhaul

Read `docs/01-dashboard-portals/PLAN.md` first (what the portals are for and
who uses them). This phase changes how they look, feel and fail — not what
they do. `IMPLEMENTATION.md` in this directory is the task-by-task build
order; `PROGRESS.md` is the dated log; `OPEN_QUESTIONS.md` holds judgment
calls.

**Why this phase exists.** Phase 7 delivered working portals, built to prove
the flows. They are now being shown to people, and the owner's verdict on the
look was blunt: it reads as a cheap, dated admin template. Two real bugs
also surfaced in use (§2), and the markup is one-off Tailwind classes with no
design system behind it, so every page looks slightly different and nothing
is cheap to extend. The goal is a product that looks professional in a demo
and that a new page or feature can be added to without reinventing styling.

## 1. Goals

1. **Look professional.** A restrained, modern infrastructure-product
   aesthetic (think the dashboards of Vercel, Linear, Railway, Fly): quiet
   neutral surfaces, one accent, strong typographic hierarchy, generous
   spacing, real empty/loading/error states, subtle motion.
2. **Open to extension.** A small design system — tokens first, then
   primitives, then composed components — so pages are assembled from parts.
   Adding a page, a nav item, a status or a theme touches one place.
3. **Never overflow.** Long identifiers (task IDs, node IDs, image digests,
   hostnames, commands) must never break a layout. They truncate with an
   ellipsis, show the full value on hover/focus, and copy with one click.
4. **Fix the two bugs** (§2) at the root, with regression tests.
5. **Fail gracefully.** A deep link refreshed in the browser, an expired
   session, a failed request or a render error each end in a designed screen,
   never a browser error page or a blank page.

Non-goals: new product features, new backend endpoints beyond what the two
fixes need, a mobile app, i18n, a public marketing site.

## 2. The two bugs, with root causes (found 2026-10-06)

**Refreshing a deep link breaks the page** ("default Chrome failure page").
`webassets.Handler` serves the SPA fallback by rewriting the request path to
`/index.html` and handing it to `http.FileServer`, which *always* answers a
request ending in `/index.html` with `301 Location: ./`. For `/tasks/tsk_123`
that resolves to `/tasks/`, which falls back again, which redirects again:
an infinite redirect loop, and Chrome stops with ERR_TOO_MANY_REDIRECTS.
Verified against the live server: ten redirects, still 301. Fix: serve
`index.html`'s bytes directly instead of through FileServer, with correct
cache headers (`no-cache` for the HTML shell, long-lived immutable for the
hashed assets). Client side: an error boundary and a designed 404 for
unknown routes so the app itself never white-screens.

**A failed task's logs keep printing the same lines.** `handleTaskLogs`
closes its SSE stream once the task is terminal and drained. The browser's
`EventSource` treats any closed stream as a dropped connection and
reconnects after ~3s; the server replays from sequence 0; the client appends
every line again, forever. Fix on both sides: the server tags each frame with
`id: <seq>`, honours `Last-Event-ID` so a reconnect resumes instead of
replaying, and sends an explicit terminal `event: end` frame; the client
closes the `EventSource` on `end`, de-duplicates by `seq`, and caps retained
lines.

## 3. Design direction

- **Theme.** Dark by default (the product is for people who run compute) with
  a full light theme; follows the OS setting, user-overridable, persisted.
  Colors are CSS variables (semantic tokens: `--bg`, `--surface`,
  `--surface-raised`, `--border`, `--text`, `--text-muted`, `--accent`,
  `--success`, `--warning`, `--danger`, `--info`), mapped into Tailwind, so a
  theme or a rebrand is a change to one file. Components never use raw hex.
- **Type.** Inter for UI, JetBrains Mono for identifiers and logs, both
  self-hosted through `@fontsource` packages — no third-party CDN, so the
  coordinator stays a single self-contained binary and demos work offline.
  A fixed scale; tabular numerals for every number.
- **Layout.** Fixed left sidebar (collapsible to icons) with grouped nav,
  a slim top bar (breadcrumbs, live-connection indicator, theme toggle, user
  menu), a centered content column with a consistent page header (title,
  description, primary action). Responsive down to phone width.
- **Components.** Cards with 1px borders and soft elevation instead of heavy
  shadows; status pills with a leading dot; tables with sticky headers, row
  hover, truncation and skeleton rows; stat cards with a trend sparkline;
  tabs; dialogs; toasts; tooltips; copy-to-clipboard everywhere an ID or
  command appears; a terminal-grade log viewer (line numbers, timestamps,
  follow toggle, wrap toggle, copy/download, stderr tinting, search).
- **Icons.** `lucide-react` (tree-shaken), replacing the hand-rolled icon file.
- **Motion.** 120–200ms transitions on color/opacity/transform only;
  honours `prefers-reduced-motion`.
- **Accessibility floor.** Visible focus rings, ≥4.5:1 text contrast in both
  themes, keyboard-operable dialogs/menus/tabs, labelled controls.

## 4. Technical decisions

- **Stay on React + TypeScript + Vite + Tailwind** (already the toolchain; a
  rewrite in another framework buys nothing). Tailwind stays at v3 to avoid a
  build-system migration inside a design phase; the token layer is plain CSS
  variables so a later move to v4 is mechanical.
- **No component library** (no MUI/Chakra/shadcn dependency tree): a small
  in-repo set of primitives under `web/src/ui/`, built on Radix primitives
  only where accessibility is genuinely hard (dialog, dropdown menu, tabs,
  tooltip), so the bespoke code stays small and the behaviour correct.
- **Routing/state unchanged**: react-router, the existing `api.ts`, `auth.tsx`
  and SSE hooks keep their contracts, so the backend and the e2e tests are
  not disturbed. Pages are rewritten; data flow is not.
- **One source of truth for domain display**: a `status` registry mapping each
  task/node/gateway state to label, tone and icon, so a new state is one line.
- **Single binary preserved**: `npm run build` output still `go:embed`s into
  the coordinator.

## 5. How it is verified

Visual work is only done when it has been looked at. A script drives headless
Chrome (puppeteer-core against the system Chrome) over a local coordinator
seeded with realistic data and captures every page at phone/tablet/desktop
widths in both themes, including empty and error states. Automated checks:
`tsc`, ESLint, the production build, Go tests for the two server fixes, and a
browser test that deep-links into `/tasks/<id>`, refreshes, and asserts the
page renders (not a redirect loop) and that a terminal task's log stream
ends and does not repeat. An overflow audit script flags any element wider
than the viewport and any ID rendered without truncation.

## 6. Out of scope / follow-ups

Command palette, notifications centre, per-task resource charts over time
(needs a metrics endpoint), team/org switcher, saved views. The component
structure leaves room for each; none is built here.
