# Store and database (`internal/coordinator/store`, `migrations/`)

Back to [README](README.md).

## Seam
`store.Store` (in `store.go`, types in `types.go`) is the only persistence interface; every coordinator package depends on it. `PostgresStore` (pgx pool) implements it, split by area: `postgres.go` (nodes, tokens, accounts), `postgres_tasks.go` (queue, `ClaimQueuedTask`, transitions), `postgres_meters.go`, `postgres_ledger.go`, `postgres_holds.go`, `postgres_logs.go`, `postgres_imagecache.go`, `postgres_gateways.go`, `postgres_portal.go` (credentials, sessions), `postgres_traffic.go` (gateway traffic series), `postgres_usage.go` (machine/task usage series). Tests use fakes (`api/fakestore_test.go`) or real Postgres via `LAZYCAKE_TEST_DATABASE_URL`. **Adding a method:** add to the interface, the Postgres impl, **and the fake store in `api/fakestore_test.go`** (and any other fake), plus a Postgres test.

Task states: `queued, reserved, dispatched, running, succeeded, failed, fenced, abandoned, cancelled`. Transitions go through `TransitionTask(id, fromStates, to, update)`; `ErrConflict` means it was not in a `from` state (someone else moved it) and callers treat that as benign.

## Migrations (goose, `migrations/NNN_name.sql`, latest **026**)
001 accounts/tokens · 002 nodes · 003 tasks · 004 logs · 005 image cache · 006 gateways · 007 tunnel targets · 008 node `instance_id` · 009 task meters · 010 ledger · 011 holds · 012 portal credentials · 013 sessions · 014/015 `node_id` ON DELETE SET NULL (so removing a machine keeps history) · 016 cancel requested · 017 gateway traffic · 018 node usage · 019 usage network + task samples · 020 fine-resolution series (raw heartbeats 48 h, 10 s gateway samples) · 021 per-gateway task samples · 022 node offer network · 023 `nodes.agent_version`, `gateways.version` · 026 `gateways.max_mbps` (INTEGER, NULL = never reported, 0 = unlimited; set from `GatewayHello.max_mbps`) · 025 `nodes.machine_id` (unique per account where not NULL; see caveats #1) · 024 `gateways.archived_at` (soft delete) and `gateways.token_hash` (links a gateway to its install token so deleting it revokes exactly that token; NULL for gateways created before 024). `ArchiveGateway` is one transaction: refuses with `ErrGatewayInUse` while a queued/reserved/dispatched/running task lists the gateway in `gateway_ids`, then archives and revokes the token. `GetGateway` still returns archived rows (check `ArchivedAt`); `ListGatewaysByAccount` hides them.

Rules: each file has `-- +goose Up` and `Down`; new columns need defaults so the **old coordinator keeps working** while the migration runs first in a deploy. Apply locally with `make migrate`; production per CLAUDE.md. Data retention: raw heartbeat usage 48 h, task samples and gateway samples 30 days.

## Production DB access
Postgres runs in container `lazycake-postgres` on the server, localhost only. Use `ssh $PROD_HOST 'podman exec -i lazycake-postgres psql -U lazycake -d lazycake ...'` (pass SQL on stdin to avoid quoting problems). The owner connects with a GUI through an SSH tunnel with their key. Take a CSV/dump backup before any data change and report counts first.

## Caveats
- `nodes` has one row per *machine* (`machine_id`; agents older than v0.4.4 still one per process instance; see [caveats](caveats.md)); `GetNodeByMachineID` and `FindLegacyNode` back registration; `DeleteNode` removes the row (and asks a connected agent to shut down).
- `trust_score` is written but never read back (see [scheduler-trust](scheduler-trust.md)).
- Version columns store whatever the agent/gateway claims, capped at 64 bytes; display only, never trusted for decisions.
