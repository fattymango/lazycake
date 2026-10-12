# End-to-end flows

See [README.md](README.md) for the picture. Module pages: [coordinator](coordinator.md), [scheduler-trust](scheduler-trust.md), [billing](billing.md), [agent](agent.md), [gateway-tunnel](gateway-tunnel.md).

## Processes

| Program | Where it runs | How it is deployed |
|---|---|---|
| `coordinator` (`cmd/coordinator`) | one production server | systemd binary; embeds the dashboard; serves portal REST `:8080`, agent gRPC `:7443`, tunnel relay `:7444`; Postgres |
| `agent` (`cmd/agent`) | every provider machine | **container**, `--restart=always`, rootless podman via its socket, `--pid=host --cap-add=SYS_ADMIN`, host networking recommended (link-speed detection) |
| `gateway` (`cmd/gateway`) | inside the customer's network | **container**, `--restart=always`; publishes named services (`LAZYCAKE_SERVICES`) |
| `lcctl`, `lcinit`, `lcbench`, `refworkload` | CLI / helpers | see [tools-cli](tools-cli.md) |

Only the agent and gateway dial **out**; nothing is pushed into customer or provider networks. That is why updates are a copy-paste command, not a push button (task 8.16).

## Task lifecycle

1. **Submit** — customer via portal (`POST /api/portal/customer/tasks`) or gRPC `CustomerService.SubmitTask`. `api.CustomerServer` validates limits, resolves image to a digest, checks the worst-case price against the available balance (`pricing.WorstCase`; `task_holds` reduce availability). State `queued`.
2. **Place** — `scheduler.Run` ticks; for each connected node `tryPlaceOne` claims a queued task that fits that node's reported free cores/memory/disk (`store.ClaimQueuedTask` with a `CapacityFilter`), honouring trust, cooldown after a rejection, cold-pull limits and the placement score. A hold is placed (`PlaceHold`) and a `Dispatch` message is sent on the agent's stream. State `dispatched`; `requeue_after` = now + lease + margin.
3. **Run** — agent `exec.Executor.HandleDispatch`: admit into the local `capacity.Ledger` (final authority), pull (skipped for a cached `@sha256:` image), create the container with limits (cgroups, no network unless tunnel targets), start → `TaskStarted` (state `running`, meter row started). Logs stream as `LogBatch`. Wall timeout, egress cap, OOM are enforced.
4. **Finish** — `TaskFinished{exit_code, exit_reason, bytes_sent/recv, cold_pull_bytes}`. Reasons: `exited`, `wall_timeout`, `oom`, `egress_exceeded`, `error`, `fenced`, `cancelled`, `stopped` (customer-requested). `scheduler.OnTaskFinished` maps to a terminal state (`succeeded`/`failed`/`fenced`/`cancelled`), then `billing.Meters.OnTaskFinished` closes the meter and settles through the ledger, trust and spec-drift are updated.
5. **Never started** (bad image, rejection): no meter row; `OnTaskFinished` releases the hold (fixed in v0.4.0; before, holds leaked).
6. **Cancel** — customer cancel sets `cancel_requested_at`; scheduler resends `Cancel` every 5 s until the agent confirms; agent stops the container and reports `cancelled`, which the coordinator turns into `stopped` (billable) when the customer asked.

## Lease and fencing (safety against double-running)

Every heartbeat (agent → coordinator, every ~15 s) is acknowledged. The agent's `conn.Runner.FenceDeadline` = last acknowledged heartbeat send time + `lease_s`; `lease.Watcher` kills every container when it passes (reason `fenced`). The coordinator reclaims a node's tasks (`reclaimOverdue`: `abandoned` for the default `at_most_once` delivery, back to `queued` for `at_least_once` with attempts left) only after `lease + margin`, so the agent is always fenced **before** a task can run elsewhere. The runner also treats 3 heartbeat intervals without any ack as a dead connection and reconnects (`AckTimeout`); dialing uses a TCP user timeout of 30 s so a dead socket after laptop suspend is replaced. A `TaskFinished` produced while there is no connection is kept by the executor (`pending`) and re-sent after the next registration (the runner installs its send channel *before* `OnRegistered` runs — do not reorder; test: `TestSelfFence`).

## Reconnect and adoption

On `Register` the agent lists `running_task_ids`; `api.adoptOrCancel` keeps those still assigned to this node and sends `Cancel` for the rest. A node is identified by `(account, machine_id)` (the agent keeps the machine id on the host, so restarts and updates are the same node); `instance_id` is minted per process and only distinguishes a restart from a reconnect. When a known machine registers as a **new process** (instance id changed), `OnAgentRestarted` reclaims every task still assigned to the node that the agent does not list (its containers died with the old process), through the same `reclaimTask` path as an expired lease. Agents without a machine id fall back to `(account, instance_id)` (see [caveats](caveats.md)).

## Tunnel (task reaches the customer's private services)

Task container runs `--network=none`. `agent/netns` joins its network namespace and runs a stub DNS (answers only declared target hostnames, each on its own loopback IP) plus a TCP listener per target. Each connection opens a QUIC stream to the coordinator relay (`tunnel/quic`), carrying a Noise_IK session (`tunnel/noise`) end-to-end to the named gateway; the relay only reads a small header (gateway, task) and pumps bytes. The gateway `listener` terminates Noise and forwards only to services in its published list. Bytes are counted by agent, relay and gateway independently and reconciled (see [billing](billing.md)). The gateway reports byte counts to the coordinator over `GatewayService.ReportBytes` (batched: one report per task+service every 5 s).

## Version and update indicator (8.16)

Agent sends `Register.version`; gateway calls `GatewayService.Hello` after connecting to the relay. Stored in `nodes.agent_version` / `gateways.version` (capped at 64 bytes, UTF-8 safe). Portal adds `version`, `latest_version` (= **`version.AgentRelease`** for machines / **`version.GatewayRelease`** for gateways, the newest release that changed that program's code; NOT the coordinator's own version and NOT the other program's, no outside lookup) and `update_state` (`current`/`outdated`/`unknown`) via `portalapi/version_info.go` using `internal/version` (numeric semver base compare; unreadable = unknown, never "outdated"). The two are independent on purpose (since v0.4.11): a dashboard-only or coordinator-only release must not make every agent and gateway read "Update available". **Bump the constant of the program whose code you edit (and re-record its hash in `internal/version/agentgateway_test.go`) in the same release**: `TestAgentReleaseIsBumpedWithItsCode` hashes `cmd/agent`, `internal/agent` and the agent generated messages, `TestGatewayReleaseIsBumpedWithItsCode` hashes `cmd/gateway`, `internal/gateway` and the gateway generated messages, both plus the shared `internal/id`, `internal/logging`, `internal/transport`, `internal/tunnel/noise` and the non-relay `internal/tunnel/quic` files; each fails until you do. A gateway-only change therefore never flags agents, and vice versa. The dashboard's `UpdateNotice` shows the badge and the copy-paste command.

## Startup order

Coordinator: load config → connect DB → (optional dev seeding) → start gRPC (agents), relay, HTTP (portal + embedded web) → scheduler loop, reclaimer, cancel resender. Agent: config → capability probe (`probe`) → offer validated against the machine (`capacity.ValidateOffer`, rejects, never clamps) → reconcile sweep (kill leftover containers of previous instances/boots) → connect/register → benchmark if asked → heartbeats, capacity reports, usage samples.
