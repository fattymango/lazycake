# Progress

One line per completed task: `<phase>.<task> — <what> — <date> — <commit>`

- 0.1 — repo skeleton, 4 binaries build + --version — 2026-09-24 — 7f...
- 0.2 — Postgres via Podman + goose migrations 001-005, up/down verified against a live database — 2026-09-24
- 0.3 — buf + protoc-gen-go/-grpc generate `internal/proto/lazycake/v1`, `go build ./...` clean — 2026-09-24
- 0.4 — per-binary Config from `LAZYCAKE_*` env, redacted `slog` logging, SIGTERM shutdown — 2026-09-24
- 1.1 — `store.Store` interface + `PostgresStore`, integration tests pass against live Postgres (run inside the podman-machine-default WSL VM) — 2026-09-24
- 1.2 — `ClaimQueuedTask` via `SELECT ... FOR UPDATE SKIP LOCKED`, 20 concurrent claimers vs 20 tasks each claimed exactly once (`-race`, passing) — 2026-09-24

