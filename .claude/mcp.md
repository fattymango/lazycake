# MCP server and OAuth (AI clients)

Back to [README](README.md). Code: `internal/coordinator/portalapi/{oauth.go,mcp.go}`, store `postgres_oauth.go`, migration 027, web `features/auth/Authorize.tsx` and `features/account/ConnectedApps.tsx`. Tests: `portalapi/mcp_test.go`.

## What it is
An AI app (Claude, Cursor, any MCP client) uses LazyCake through the **MCP** protocol at `https://<host>/mcp`. The user adds that one URL; there is no token to paste. The client opens the dashboard's `/authorize` page, the user (signed in, or signing in first) clicks **Allow**, and the client is connected. It then acts **as that account** (customer or provider, fixed by the account's role). Spending is the user's own decision: there is no spend cap.

## The flow (OAuth 2.1, public client, PKCE S256 required)
1. `POST /mcp` without a token → `401` with `WWW-Authenticate: Bearer resource_metadata=".../.well-known/oauth-protected-resource"`.
2. Discovery: `GET /.well-known/oauth-protected-resource` (RFC 9728) and `/.well-known/oauth-authorization-server` (RFC 8414). `PublicBase` is `LAZYCAKE_PUBLIC_URL` if set, else read from the request (`X-Forwarded-Proto/Host`).
3. `POST /oauth/register` (RFC 7591, open, rate limited with the sign-up bucket: 5 per 10 min per address). Redirect URIs must be `https`, loopback `http` (any port allowed at authorize time) or an app scheme (`cursor://`…); never `javascript:`/`data:`/fragments. A registered client can do nothing until approved.
4. The client sends the browser to `/authorize?client_id&redirect_uri&code_challenge&code_challenge_method=S256&state` (the SPA route, `RequireAuth`-guarded so it passes through sign-in). The page asks `GET /api/portal/oauth/client` (validates client + exact redirect) then `POST /api/portal/oauth/authorize {…, approve}` → `{redirect_to}`; the browser goes back to the app with `?code=` (or `?error=access_denied`). **Both consent routes use `requireAnySession` (cookie only): a bearer token can never approve anything.**
5. `POST /oauth/token` (form-encoded): `authorization_code` (+ `code_verifier`, `redirect_uri`, `client_id`) or `refresh_token`. Code: 5 min, single use (`DELETE … RETURNING`). Access token `lca_…` 1 h, refresh token `lcr_…` 30 days **rotated on every refresh** (the old pair stops working); both stored as sha256 hashes. Token requests share the login rate limit.
6. `POST /mcp` with `Authorization: Bearer lca_…`. Transport: streamable HTTP, JSON responses only (no SSE, no server push, no sessions; GET → 405). Methods: `initialize`, `ping`, `tools/list`, `tools/call`; notifications get 202.

## Auth plumbing
`requireSession(role, …)` accepts either the session cookie **or** a bearer access token (`bearerSession` → `Store.GetOAuthTokenByAccess`, which also stamps `last_used_at`); the role check applies to both. So an OAuth token can call the whole REST API for its account, not only the MCP tools. `requireAnySession` stays cookie-only on purpose.

## Tools (`mcpTools` in `mcp.go`; per role, listed by `tools/list`)
- **customer:** `get_account`, `task_overview`, `submit_task`, `list_tasks`, `get_task`, `get_task_logs`, `task_usage`, `cancel_task`, `list_gateways`, `create_gateway`, `billing_ledger`.
- **provider:** `get_account`, `list_machines`, `get_machine`, `machine_tasks`, `earnings_ledger`, `add_machine_command`.
- A tool is a thin wrapper that **calls the REST handler in-process** (`mcpCall.rest`: same token, same remote address for rate limits) so ownership, balance and validation rules live in one place. Exceptions: `list_tasks` filters the REST list; `get_task_logs` reads `Store.ListLogs` (the REST route is SSE) after an ownership check and labels the lines as untrusted program output (prompt-injection hygiene).
- Deliberately **not** exposed: the dev add-funds faucet, deleting gateways/machines, signing in or out.
- A tool failure is a normal result with `isError: true` (the model sees why); an unknown or wrong-role tool is a JSON-RPC error. Output is cut at 100 KB.
- Add a tool: append to `mcpTools` with a schema built by `obj(...)` (**`properties` must be an object, never null**: `TestEveryToolHasAValidInputSchema`; a null made the official SDK reject the whole tool list), then extend `mcp_test.go`.

## Users' side
`/apps` ("Connected apps", both roles): shows the MCP URL and every connected app with last use, and **Disconnect** (`DELETE /api/portal/oauth/grants/{id}`) revokes the token at once. API: `GET /api/portal/oauth/grants`.

## Caveats
- **HTTPS** is available on the same port since v0.4.25 ([https](https.md)); use `https://<host>:8080/mcp`. Over plain `http://` tokens and codes travel in clear text (with `LAZYCAKE_FORCE_HTTPS=1` plain visitors are redirected).
- Migrations: 027 must be applied **before** the coordinator that uses it.
- OAuth ids use a local `newID` (random hex), **not** `internal/id`: that package is hashed by the agent/gateway release guard (`internal/version/agentgateway_test.go`).
- Verified with the official `@modelcontextprotocol/sdk` client end to end (discovery, registration, real browser consent, token exchange, tool calls); keep doing that after protocol changes (a throwaway script outside the repo is enough).
