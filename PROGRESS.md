# Progress

One line per completed task: `<phase>.<task> — <what> — <date> — <commit>`

- 0.1 — repo skeleton, 4 binaries build + --version — 2026-09-24 — 7f...
- 0.2 — Postgres via Podman + goose migrations 001-005, up/down verified against a live database — 2026-09-24
- 0.3 — buf + protoc-gen-go/-grpc generate `internal/proto/lazycake/v1`, `go build ./...` clean — 2026-09-24
- 0.4 — per-binary Config from `LAZYCAKE_*` env, redacted `slog` logging, SIGTERM shutdown — 2026-09-24
- 1.1 — `store.Store` interface + `PostgresStore`, integration tests pass against live Postgres (run inside the podman-machine-default WSL VM) — 2026-09-24
- 1.2 — `ClaimQueuedTask` via `SELECT ... FOR UPDATE SKIP LOCKED`, 20 concurrent claimers vs 20 tasks each claimed exactly once (`-race`, passing) — 2026-09-24
- 1.3 — `api.Server` implements `AgentService.Connect`: auth, register, heartbeat ack, capacity/cache/log handling, task-event hooks behind a `TaskEvents` interface the scheduler will implement later — 2026-09-24
- 1.4 — `conn.Runner` (register/heartbeat/reconnect w/ jittered backoff) + real wiring in cmd/coordinator and cmd/agent; manual kill -9 restart test: agent reconnects in ~10s — 2026-09-24
- 1.5 — `agent probe`: real memory/cpu/pids/disk/systemd/gvisor/subuid/cgroup checks against actual podman, not version strings; ran for real inside podman-machine-default (mixed pass/fail, all actionable) plus full fake-Runner unit coverage — 2026-09-24
- 1.6 — `runtime.Runtime` interface + `PodmanRuntime` (docker/docker/client v24 pinned against the podman API socket); real integration test: pull alpine, run+wait+read logs+remove all pass. The OOM sub-test fails on this VM for the same cgroupfs-fallback reason task 1.5 already found (not a code bug, see OPEN_QUESTIONS.md) — 2026-09-24
- 1.7 — `capacity.Ledger`: per-task-ID admission control (never overcommits, concurrency-safe, `-race` clean), `ClampOffer` for the 75%-cores/headroom-reserved rule, `SetOffer` drains rather than evicts per PLAN.md — 2026-09-24
- 1.8 — full dispatch loop: `scheduler` places queued tasks onto connected agents, `exec.Executor` runs them for real (admission, pull, create/start, wait, report). `internal/e2e` proves it against real Postgres+podman: submit -> succeeded with exit 0 works end to end. OOM sub-test fails on this VM for the already-documented cgroup-delegation reason, not a code bug — 2026-09-24

