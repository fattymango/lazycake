# Wire protocol (`proto/lazycake/v1/*.proto` → `internal/proto/lazycake/v1`)

Back to [README](README.md). Generated with `make proto` (`buf generate`; needs `buf`, `protoc-gen-go`, `protoc-gen-go-grpc` on PATH). **Edit the `.proto`, regenerate, commit the generated `.pb.go` files together.** Never hand-edit `.pb.go`. Field numbers are forever: add new fields with new numbers (old binaries keep working — agents and gateways are updated by their owners at their own pace, so every change must be backward compatible in both directions).

## `agent.proto` — `AgentService.Connect(stream AgentMessage) returns (stream CoordinatorMessage)`
- **Agent → coordinator** (`AgentMessage` oneof): `Register` (token, hostname, arch, cpu_flags, `Capabilities`, `Offer`, boot_id, instance_id, cached images, `running_task_ids`, **`version` = 11**, **`machine_id` = 12**: stable per physical machine, empty from an agent older than v0.4.4), `Heartbeat` (seq, running ids, optional `UsageSample`/`TaskUsage`), `CapacityReport` (offer + free cores/memory/disk), `CacheDelta`, `TaskAccepted`, `TaskRejected`, `TaskStarted`, `TaskFinished` (exit code/reason, bytes sent/recv, cold-pull bytes), `LogBatch`, `BenchReport`.
- **Coordinator → agent** (`CoordinatorMessage` oneof): `RegisterAck` (node_id, heartbeat_s, **lease_s**, benchmark_now), `HeartbeatAck` (seq), `Dispatch` (task id, digest-pinned image, entrypoint/args/env/workdir, `Limits`, isolation, `TunnelTarget`s, lease expiry, attempt), `Cancel`, `Shutdown`.
- `Offer.network_mbps` = tunnel bandwidth cap; `Offer.bandwidth_mb_month` is defined but **unused**.
- `Limits`: cpu_cores, memory_mb, disk_mb, tmpfs_mb, pids, wall_timeout_s, no_output_timeout_s, egress_mb.

## `customer.proto` — `CustomerService`
`SubmitTask`, `GetTask`, `CancelTask`, `StreamLogs` (server stream), `ListNodes`, `CreateGateway`, `ListGateways`; `TaskLimits`, `TunnelTargetSpec`, `TaskStatus`, `NodeStatus`, `GatewayStatus`. Used by `lcctl` and (via shared logic) the portal.

## `gateway.proto` — `GatewayService`
`ReportBytes(ByteReport)` (task, service, bytes, `connections` count; legacy per-connection final=1), `Hello(GatewayHello{gateway_id, version})`.

## Rules
- Auth is a bearer token in gRPC metadata (`api/bearer.go`), kind-checked per service.
- `go_package` option is `github.com/fattymango/lazycake/internal/proto/lazycakev1`; keep it matching the module path.
- When adding a message handled by the coordinator also update: the handler in `api/server.go`, the fake agent/store in tests, and this page.
