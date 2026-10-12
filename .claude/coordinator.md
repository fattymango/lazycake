# Coordinator (`cmd/coordinator`, `internal/coordinator/*`)

One Go process: gRPC for agents/customers/gateways, REST for the dashboards, QUIC relay for tunnels, background scheduler. Back to [README](README.md). Related: [scheduler-trust](scheduler-trust.md), [billing](billing.md), [store-database](store-database.md), [portal-api](portal-api.md).

## Abuse protection
`internal/coordinator/ratelimit`: `Limiter` (token bucket per IP) and `FailGuard` (N failed authentications per window blocks an IP). HTTP: see [portal-api](portal-api.md); the server has `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s, `IdleTimeout` 2 min, 64 KiB headers, and deliberately **no `WriteTimeout`** (SSE streams). gRPC (7443): unary and stream interceptors refuse an IP with `ResourceExhausted` after 20 `Unauthenticated` answers in a minute (5-minute block). Relay (7444): the same `FailGuard` (shared count) via `quic.Relay.Guard`; a blocked address is dropped before it can authenticate. All in memory: a restart forgets it. Not a volumetric-DDoS defence: that needs something in front of the box.

## `cmd/coordinator`
`main.go` (flags, `--version`), `run.go` (wiring of every component and the gRPC servers; heartbeat and lease seconds are set here), `shutdown.go` (graceful stop; gRPC graceful stop is forced after 10 s because agent streams stay open — the "graceful stop timed out" warning on restart is normal).

## Config (`internal/coordinator/config`) — environment variables
`LAZYCAKE_DATABASE_URL`, `LAZYCAKE_GRPC_ADDR` (`:7443`), `LAZYCAKE_HTTP_ADDR` (`:8080`), `LAZYCAKE_RELAY_ADDR` (`:7444`), `LAZYCAKE_PUBLIC_GRPC_ADDR` (address shown in install commands), `LAZYCAKE_TLS_HOST`/`LAZYCAKE_TLS_DIR`/`LAZYCAKE_TLS_ACME_DIRECTORY`/`LAZYCAKE_TLS_CHALLENGE_ADDR`/`LAZYCAKE_FORCE_HTTPS` (HTTPS on the same port, see [https](https.md)), `LAZYCAKE_PUBLIC_URL` (the https://host used in the OAuth/MCP discovery documents; empty = derived from the request), `LAZYCAKE_DEV` (dev mode), `LAZYCAKE_SEED_DEMO_TOKEN`, `LAZYCAKE_SEED_PORTAL_CUSTOMER_USERNAME`, `LAZYCAKE_SEED_PORTAL_PROVIDER_USERNAME`, `LAZYCAKE_SEED_PORTAL_PASSWORD`. Secrets in config are masked in the startup log.

## `api` — gRPC servers
- `server.go` `AgentService.Connect`: authenticate token (`auth.Hash` → `Store.Authenticate`, kind must be `agent`), `handleRegister` (node row by `(account, instance_id)`, stores offer/caps/version), `adoptOrCancel`, then a receive loop dispatching `handleMessage`: heartbeat (also stores usage samples, extends lease → `requeue_after`), `CapacityReport` (`handleCapacity`: skips `SetNodeOffer` DB write when unchanged, feeds scheduler free-capacity), cache delta, accepted/rejected/started/finished, logs, bench report.
- `registry.go` `Registry`: live streams per node; `Send(nodeID, msg)`; satisfies the scheduler's `Dispatcher` interface.
- `customer_server.go` `CustomerService` (submit, get, cancel, stream logs, list nodes, create/list gateways) shared by gRPC and the portal; `customer_cancel.go` cancel logic.
- `gateway_server.go` `GatewayService`: `ReportBytes` (batched, `Connections` count; legacy final=1) and `Hello`. Token must be kind `gateway`; gateway must belong to the token's account.
- `bearer.go` bearer-token extraction; `errors.go` status mapping.
- Events other packages receive are defined here (`TaskFinishedEvent`, etc.): `scheduler` and `billing` implement the callbacks; `api` never imports them.

## `auth`
Tokens are stored only as hashes; `auth.Hash` used on write and read. Token kinds (`store.TokenKind`): `customer`, `agent`, `gateway`; each RPC checks the kind.

## `events`
Tiny in-process pub/sub for SSE (task state, node connect/disconnect, capacity). Publishers (`api`, `scheduler`) depend on it, not vice versa.

## `relay`
Adapter giving `tunnel/quic.Relay` an authenticator and gateway registry backed by the store, so the transport package never imports coordinator code. Records relay-side byte counts for reconciliation.

## `seed`
Creates demo accounts/tokens only when explicit env vars are set (dev/demo). Production also has seeded portal demo accounts; there is no public signup flow beyond the portal's signup endpoints.

## `pricing`, `billing`, `scheduler`, `store`, `portalapi`
Own pages: [billing](billing.md), [scheduler-trust](scheduler-trust.md), [store-database](store-database.md), [portal-api](portal-api.md).

## `webassets`
`go:embed` of `internal/coordinator/webassets/dist` (the built dashboard; **gitignored except `.gitkeep`**). Build the web app into that directory *before* building the coordinator, or the binary serves nothing. `dashboard/` is the older single-file ops page.

## Caveats
- Trust scores live in memory (restart → 0.5 for everyone). See [caveats](caveats.md).
- The coordinator runs on a 1 vCPU / ~458 MB box: never do per-event DB transactions on hot paths (this once starved heartbeats — gateway reports are batched for that reason).
- Version comes from `internal/version.Version`, set only by `-ldflags -X` (see CLAUDE.md for the module path).
