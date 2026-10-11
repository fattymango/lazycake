# Portal REST API (`internal/coordinator/portalapi`)

Plain REST/JSON under `/api/portal/*`, backing the customer and provider web apps. Back to [README](README.md). It wraps the same `store.Store` / `api.CustomerServer` logic as gRPC rather than re-implementing it. Frontend consumer: [web-frontend](web-frontend.md).

## Auth and sessions
- Username/password accounts (portal credentials table, migration 012); sign-up for customer and provider, `POST /api/portal/login` (unified), role-specific logins. Sessions in `sessions` (013), httpOnly `SameSite=Lax` cookie, 7-day TTL, `Secure` unless dev mode. `lockout.go` throttles repeated failed logins. Account-scoped: every handler uses the session's `AccountID`; never trust IDs from the client without `getOwn…` checks.
- Production is **plain HTTP** today, so `Secure` cookies behave per config; HTTPS is backlog 8.18.

## Routes (see `server.go` for the authoritative list)
- Auth: `POST …/customer/signup|login`, `…/provider/signup|login`, `POST /login`, `POST /logout`, `GET /me`.
- Customer: `tasks` (POST submit, GET list, `{id}` GET, `{id}/cancel`, `{id}/logs`, `{id}/usage`, `{id}/traffic`), `gateways` (GET/POST, `{id}/traffic`, `{id}/test`), `ledger`, `balance/add` (dev-style top-up, there is no payment rail), `events` (SSE), `me`.
- Provider: `nodes` (GET list, `{id}`, DELETE `{id}` = remove machine + ask agent to exit, `{id}/usage`, `{id}/tasks`, `nodes/install-token` POST = mint an agent token and return the install command for the chosen offer), `ledger`, `events` (SSE), `me`.

## Files
`auth_handlers.go` login/signup/me · `customer_handlers.go` · `provider_handlers.go` (`toNodeView`, install command, offer validation) · `traffic_handlers.go` / `usage_handlers.go` (time-series: 5-minute buckets, raw 30 s heartbeat rows for the 1-hour view, 10 s gateway samples) · `events_handler.go` + `sse.go` (one SSE stream per tab fed by `events`) · `version_info.go` (`versionView{version, latest_version, update_state}` embedded in `nodeView` and `gatewayView`) · `json.go` helpers · `lockout.go` · `session.go`.

## Behaviors to preserve
- **Install-token validation:** cores 0.25–1024, memory ≥ 256 MB, storage ≥ 1 GB (whole GB), network 1–100,000 Mbps; the server rejects fractions/non-whole-GB sizes. The returned command uses `docker.io/fattymango/lazycake-agent:latest`, `--replace --name lazycake-agent`, `--restart=always`, `--pid=host --cap-add=SYS_ADMIN`, host networking, `LAZYCAKE_TOKEN` and the `LAZYCAKE_OFFER_*` variables.
- **Show measurements exactly** (no rounding that hides small CPU use) — the owner's transparency rule.
- Machine and gateway JSON include the version fields; gateway test (`…/test`) returns a four-state result (`green/yellow/red/grey`; grey = old gateway that cannot verify services).
- Serve the SPA with deep-link fallback (refreshing `/tasks/…` must load the app; the Go file server once broke this — covered by `web/e2e/behaviour.mjs`).
