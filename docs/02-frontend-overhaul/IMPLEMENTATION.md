# LazyCake Phase 8 — Frontend Overhaul: Implementation Guide

Read `PLAN.md` in this directory first. Same rules as
`docs/00-core-platform/IMPLEMENTATION.md` §0: one task at a time, verify
before moving on, commit as `8.<task>: <description>` (plain messages, no
`Co-Authored-By` trailer — see repo `HANDOVER.md`), keep `PROGRESS.md`
updated, log judgment calls in `OPEN_QUESTIONS.md`. Visual tasks are not done
until screenshots of the result have been inspected (task 8.10's script).

---

## 8A — Root-cause fixes (server)

### Task 8.1 — SPA fallback no longer redirect-loops

**Files:** `internal/coordinator/webassets/webassets.go` (+ test).
Serve `index.html`'s bytes directly for any non-asset path; never route the
fallback through `http.FileServer`. `Cache-Control: no-cache` on the HTML,
`public, max-age=31536000, immutable` on `/assets/*`. Unknown `/api/*` paths
must still 404 as JSON, not return the shell.
**Verify:** a Go test requesting `/tasks/tsk_x`, `/gateways`, `/` and
`/assets/<file>` gets 200 with the right body and no redirect; a request for
`/api/portal/nope` is not the shell.

### Task 8.2 — Log stream resumes instead of replaying, and says when it's done

**Files:** `internal/coordinator/portalapi/customer_handlers.go` (+ test).
Emit `id: <seq>` per frame; start from `Last-Event-ID` when present; when the
task is terminal and drained send `event: end\ndata: {}\n\n` then close.
**Verify:** a test streams a terminal task's logs, reconnects with
`Last-Event-ID`, and receives nothing new; the first stream ends with the
`end` event.

## 8B — Design foundation

### Task 8.3 — Tokens, fonts, Tailwind mapping, icons

**Files:** `web/src/ui/tokens.css`, `web/tailwind.config.js`, `web/package.json`.
Semantic CSS variables for dark and light (`:root`, `[data-theme=light]`,
OS-preference default), Tailwind colors mapped to `rgb(var(--…) / <alpha>)`,
`@fontsource-variable/inter` + `jetbrains-mono`, `lucide-react`, reduced
motion, scrollbar and selection styling. **Verify:** build passes; no raw hex
outside `tokens.css`.

### Task 8.4 — Primitives

**Files:** `web/src/ui/*`. Button (variants, sizes, loading), IconButton,
Input/Textarea/Select/Switch/Field, Card, Badge, Skeleton, Spinner, Tooltip,
Dialog, DropdownMenu, Tabs (Radix-backed), Toast system, `cn()` helper.
**Verify:** a `/_kit` dev-only route renders every primitive in both themes.

### Task 8.5 — Display components

**Files:** `web/src/ui/*`. `TruncatedText`/`Identifier` (ellipsis, tooltip,
copy), `CodeBlock` (copy), `StatCard` with sparkline, `StatusPill` +
`status.ts` registry, `DataTable` (sticky head, skeleton, empty, truncation,
row links, optional sort), `KeyValueList`, `EmptyState`, `ErrorState`,
`PageHeader`, `Section`, `ProgressBar`. **Verify:** `/_kit` shows each with
an adversarial 120-character unbroken ID and nothing overflows.

### Task 8.6 — App shell and resilience

**Files:** `web/src/ui/AppShell.tsx`, `ErrorBoundary.tsx`, `NotFound.tsx`.
Sidebar (grouped, collapsible, mobile drawer), top bar (breadcrumbs, live
indicator, theme toggle, user menu), error boundary around routes, designed
404, session-expiry handling (a 401 anywhere routes to login with the
return path), offline banner. **Verify:** killing the API shows the designed
error state, not a blank page; an unknown route shows the 404.

## 8C — Pages

### Task 8.7 — Auth

Login and signup: split layout with brand panel, inline validation, role
choice on signup, clear errors, password visibility toggle.

### Task 8.8 — Customer portal

Dashboard (balance, recent tasks, running/queued counts, sparkline),
Submit task (presets, env-var editor, tunnel-target picker, resource
sliders with live cost hint), Tasks (search, state filter, pagination),
Task detail (header with status + actions, tabs: Overview / Logs / Spec,
lifecycle timeline, the new log viewer, end-aware stream per 8.2),
Gateways (cards, connection state, create dialog with one-time token reveal),
Billing (balance, add funds, ledger table).

### Task 8.9 — Provider portal

Dashboard (fleet summary, earnings, machine cards with capacity bars),
Add machine (stepper, copyable install command with the corrected image
name and notes), Machine detail (status, capacity, trust score, recent tasks,
remove), Earnings (totals + ledger).

## 8D — Verification and handover

### Task 8.10 — Visual and behavioural verification

**Files:** `web/e2e/` (puppeteer-core), `web/e2e/seed.sql`, README section.
Seed a local coordinator + Postgres; capture every page × {390, 820, 1440}px
× {dark, light}; overflow audit; the deep-link-refresh and log-stream tests
from `PLAN.md` §5. **Verify:** all screenshots reviewed, audit clean, tests
green; screenshots of the final product committed under `docs/screenshots/`
for the README.

### Task 8.11 — Cleanup and docs

Delete the old components and styles, update `docs/README.md` and the
top-level README, record the design system's extension points (how to add a
page, a status, a theme) in `web/README.md`.
