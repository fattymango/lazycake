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
- 1.9 — `exec.logRelay`: batches at 250ms/64KB, rate-limited to 1MB/s, capped at 50MB/task with a single truncation marker, drop-not-buffer when the coordinator is slow (64-slot bounded send channel). `TestLogFlood` (real podman, `-tags=integration`): a container emitting 500MB of output relays <60MB and the task still finishes normally, 46.9s — 2026-09-24
- 1.10 — `lcctl` (submit/status/logs/nodes) against a new `CustomerService` gRPC API; `deploy/docker-compose.yml`+`Dockerfile` build and run for real under `podman compose` (Postgres, migrate, coordinator with demo-account seeding all verified up). The literal 3-agent chaos demo is `BLOCKED` on this machine: the agent binary correctly refuses to start because `agent probe` correctly detects this VM has no working cgroup delegation (same root cause as 1.5/1.6/1.8, reconfirmed twice more here) - see OPEN_QUESTIONS.md. The pipeline itself is already proven end-to-end by `internal/e2e` (task 1.8) — 2026-09-24

**Phase 1 (vertical slice) complete**, with the one documented environment caveat above.

- 2.1 — `testdata/netns-spike.sh`: proves the tunnel's core mechanism for real against podman. All 5 steps pass: `--network=none` container created; agent enters its user+net namespace unprivileged via `nsenter --user --net` (joining both together is the trick - net alone would EPERM); loopback brought up and a listener bound from the agent side; a process inside the container reaches that listener and gets its echo back; the container has no default route and DNS resolution fails outright. Rootless netns manipulation - the thing PLAN.md flags as the riskiest unknown in the whole project - genuinely works as designed — 2026-09-24
- 2.2 — `internal/tunnel/noise`: Noise_IK session over any `io.ReadWriter`, length-prefixed framing, auto-chunked at the 65519-byte Noise message limit. 10MB round-trip identical bytes; a single flipped bit anywhere in a ciphertext fails AEAD authentication on decrypt — 2026-09-24
- 2.3 — `internal/tunnel/quic`: coordinator `Relay` (agents/gateways dial in, auth via a small `Authenticator` interface so quic never imports coordinator packages), one QUIC stream per container connection tagged `{gateway_id, task_id}`, 30s idle timeout per stream, byte counting on both directions never decoding payload. `TestRelayRoundTrip` (real UDP, `-tags=integration`): agent opens a stream, gateway receives it and the right task_id, bytes flow both ways, relay's own counters match exactly what was sent — 2026-09-24

