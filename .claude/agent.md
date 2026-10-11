# Agent (`cmd/agent`, `internal/agent/*`)

Runs on a provider's machine, **as a container** (`--restart=always`), drives rootless podman through its socket, keeps one outbound gRPC stream to the coordinator. Back to [README](README.md). Flows: [architecture](architecture.md). Wire format: [protocol](protocol.md). Tunnel part: [gateway-tunnel](gateway-tunnel.md).

## `cmd/agent`
- `run.go`: wiring. Order: config → probe → `ValidateOffer` (refuses an offer bigger than the machine; message names every oversize setting; exits) → `reconcile.Sweep` → create `Executor`, `Runner`, `lease.Watcher`, usage sampler, benchmark loop, `reportCapacity` → run. Sets `Identity.Version = version.Version`. `instanceID := id.New("ins")` **per start** (sweep/adoption bookkeeping), plus `machineID` from `machineid.go` (`loadOrCreateMachineID`: `<LCINIT_HOST_DIR>/machine-id`, else `<user config>/lazycake/machine-id`, written atomically; "" if unwritable) sent in `Register`.
- `capacity.go`: `agent capacity` prints `cores= memory_mb= disk_mb= network_mbps= disk_total_mb=` (needs host networking to see real link speed); the dashboard asks providers to paste it.
- `capacityreport.go`: `reportCapacity` sends a `CapacityReport{offer, free cores/memory/disk}` at start, on change (polled every 2 s) and every 30 s so the scheduler places by free capacity.
- Subcommands: `agent probe` (capability report), `agent capacity`, `--version`.

## Config (`config`) — env
`LAZYCAKE_COORDINATOR_ADDR`, `LAZYCAKE_TOKEN`, `LAZYCAKE_RELAY_ADDR`, `LAZYCAKE_DATA_DIR`, `LAZYCAKE_OFFER_CORES`, `_MEMORY_MB`, `_DISK_MB`, `_NETWORK_MBPS` (0 = no limit), `LAZYCAKE_LCINIT_PATH`, `LAZYCAKE_LCINIT_HOST_DIR`, `LAZYCAKE_DEV`.

## `conn` — the connection (`runner.go`)
One long-lived `AgentService.Connect` stream. `Runner.Run` reconnects with `Backoff` (1 s–60 s; reset when a connection lasted at least `healthyConnection` = 30 s, so a coordinator restart costs ~1 s, not a minute that would self-fence running tasks). `runOnce`: connect → send `Register` (incl. version, running task ids) → wait `RegisterAck` → **install `sendCh`, then call `OnRegistered`** (so replayed reports can be queued) → run `sendLoop` (heartbeats, outbound queue) and `recvLoop` (dispatch/cancel/shutdown/acks). `Send` drops when there is no stream; `TrySend` reports whether it queued (used for must-not-lose messages). `AckTimeout` (default 3 × heartbeat) abandons a stream that stops being acknowledged. `FenceDeadline()` is what the watcher reads. `dial_linux.go` `Dialer` sets `TCP_USER_TIMEOUT` 30 s and keepalive 15 s so a socket dead after suspend/NAT drop is torn down and replaced. Tests inject a fake `AgentServiceClient`.

## `exec` — running tasks (`executor.go`, `logs.go`)
`HandleDispatch` → goroutine `run`: `Ledger.Admit` (reject → `TaskRejected`), `TaskAccepted`, pull (`Runtime.Pull` **skips the registry for a cached digest-pinned `@sha256:` image** — an always-pull once exhausted Docker Hub's anonymous allowance), create (limits, `--network=none`, lcinit wrapper bind-mounted when `LcinitPath` set so wall timeout is enforced from inside the container), start (`TaskStarted`), tunnel proxy if targets, wait, logs, `TaskFinished`. Exit reason decided by wall timeout, egress cap, OOM, fence/cancel overrides. **Final reports** go through `reportFinished`: kept in `pending` until a connection accepts them; `ReplayPending()` (called from `OnRegistered`) re-sends only undelivered ones. `FenceAll` and `CancelTask` stop containers (SIGTERM, grace, SIGKILL) and force the reported reason. Container name is `lazycake-<task_id>-<attempt>`. `logs.go` batches log lines (50 MB per task cap; a log flood must not stall the task).

## `runtime` (`runtime.go`, `podman.go`, `socket.go`)
`Runtime` interface over the Docker-compatible REST API (podman and docker): Pull, Create, Start, Stop, Wait, Logs, Remove, ListLabelled, Exec, Pid, Stats. `DefaultSocket()` finds the rootless socket. Containers carry labels (instance id, boot id, agent id) used by the sweep.

## `capacity` — admission and validation
`ledger.go`: the agent's own `Ledger` of free cores/memory/disk; final authority on whether a task fits (the coordinator's view is slightly stale by design). `validate.go` `ValidateOffer`: offer vs machine — cores, total RAM, **disk size** (not free space; v0.4.0; the agent only *warns* when the offer exceeds currently free disk), fastest physical link (`/sys/class/net/*/speed`, ignoring virtual interfaces; unreadable speed ⇒ unchecked). Rejects, never clamps. `hostinfo_linux.go` `Physical()`. Network offer is enforced separately as one shared token-bucket limiter over all tunnel traffic (`netns.NewBandwidthLimiter`).

## `lease` — self-fencing
`Watcher` polls `Deadline()` (every 200 ms in tests); when passed, `OnFence` → `executor.FenceAll`. Guarantee: nothing runs on an agent that can't hear the coordinator past `lease_s` + stop grace.

## `reconcile` — startup sweep
Before accepting work, kill containers left by a previous agent instance or a previous boot (labels `instance_id`/`boot_id`).

## `probe` — capability probe
Actually exercises memory limit (OOM kill), CPU quota, pids limit, disk limit, gVisor, systemd slice, runtime/cgroup version, and advertises only what it proved. Result goes into `Register.caps`.

## `usage` — heartbeat usage (display only)
Machine totals and per-task CPU/memory/tunnel counters sampled for the heartbeat; never feeds billing or trust; bounded so a stuck stats read cannot stall heartbeats.

## `bench` — benchmark
Fixed deterministic SHA-256 + matrix multiply; score vs a reference constant; sent as `BenchReport` when the coordinator asks (`benchmark_now`). `normalised_s` uses it. `TestBenchStability` is timing-sensitive (see [testing](testing.md)).

## `netns` — in-container tunnel proxy → see [gateway-tunnel](gateway-tunnel.md).

## Install form
Dashboard "Add machine" generates: `mkdir -p … && podman run -d --replace --name lazycake-agent --restart=always --pid=host --cap-add=SYS_ADMIN --network=host -e LAZYCAKE_TOKEN=… -e LAZYCAKE_OFFER_… docker.io/fattymango/lazycake-agent:latest` (exact text from `portalapi/provider_handlers.go`). Image built from `deploy/Dockerfile --target agent`. systemd user unit `deploy/systemd/lazycake-agent.service` is the non-container alternative (not used in production).
