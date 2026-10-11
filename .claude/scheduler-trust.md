# Scheduler and trust (`internal/coordinator/scheduler`)

Back to [README](README.md). Flow context: [architecture](architecture.md).

## Loop and placement
- `Scheduler.Run(ctx, interval)` ticks `tick` (every second): for every connected node, `fillNode` calls `tryPlaceOne` repeatedly (up to `maxPlacementsPerTick` = 16) until nothing fits; `tryPlaceOne` claims one queued task that fits (`store.ClaimQueuedTask(nodeID, CapacityFilter, requeueAfter)`), places a hold, sends `Dispatch` through `Dispatcher` (the `api.Registry`). Free capacity comes from the agent's `CapacityReport` (`OnCapacityReport` → `freeCap` map). **Each successful dispatch is subtracted from `freeCap` at once (`reserveFree`)**; the agent's next report overwrites it with the truth. Before v0.4.14 it was not, so a node was offered more than it had and the agent rejected ~27% of dispatches ("does not fit free capacity"); with one placement per node per tick a freed node was also refilled at 1 task/s. Cores are **reserved by the task's limits, not by what it uses**: a node can be "full" (all offered cores reserved) while actual CPU use is well below, which is by design (a task is guaranteed what it asked for); before the first report it falls back to the node's whole offer (an optimistic over-estimate is fine: the agent's own ledger is the final admission authority).
- A node that rejected a task is in `inCooldown` for that task. A banned node (trust below threshold) gets nothing; transitions are logged by `noteBan` ("stopped/resumed dispatching") — silence here once looked like a broken scheduler.
- `placementscore.go`: pure `PlacementScore` = cache bonus (image digest already cached, weight 1.0) − queue penalty (0.5) + trust (weight 2.0).
- `coldpull.go`: at most 3 nodes cold-pulling the same digest at once; released on `TaskStarted` or finish.
- `canary.go`: probabilistic canary tasks to catch hosts that lie (rate 5% for low trust down to 0.5% for high); pass +0.02, fail −0.30.
- `reclaim.go` `reclaimOverdue`: tasks past `requeue_after` (node stopped heartbeating beyond lease + margin). A customer-cancelled task ends as `stopped`. Delivery `at_most_once` (the default) goes straight to **`abandoned`**; `at_least_once` with attempts left goes back to `queued` for another node. On abandon the hold is released and the node's trust drops (−0.25). Always later than the agent's own fence deadline (invariant test: `lease_invariant_test.go`).
- `cancel.go`: resends `Cancel` every 5 s until the agent confirms; `finishStopped` finalises.
- `events.go`: `OnTaskAccepted/Rejected/Started/Finished`, `terminalState`, capacity reports. `OnTaskFinished` is **not idempotent** (a duplicate report adds trust +0.01 again and re-observes spec drift); the agent only re-sends a final report that no connection has taken, so duplicates are rare — do not make the agent replay already-sent reports.

## Trust (`trust.go`)
In `TrustTracker`, in memory, keyed by **node id**. Start 0.5, max 1.0, ban below **0.2**. Clean completion +0.01; canary pass +0.02; spec drift −0.15; byte divergence −0.20; abandoned −0.25; canary fail −0.30. **Healing:** +0.2 per hour toward (never above) the start value 0.5 — a ban is not forever. The database column `nodes.trust_score` is written in small steps and **loaded back at startup** (`TrustTracker.LoadFrom`, called in `cmd/coordinator/run.go`; test `TestTrustSurvivesARestart`), so a coordinator restart no longer resets trust. A restarted agent is a new node id and also starts at 0.5 (backlog 8.19).

## Spec drift (`specdrift.go`)
Compares a task's benchmark-normalised duration with the node's own history **for the same workload** (`WorkloadKey`: image + entrypoint + args + env + cores + memory). Only tasks that exited normally (code 0, reason `exited`); tasks under **10 normalised seconds are ignored** (start-up jitter); 5 calibration samples per workload, EWMA α 0.3, flag at ratio 1.8, at most 256 workloads per node. (Earlier versions compared against the node's first 5 tasks of any kind and banned honest machines under mixed load — v0.3.8/v0.3.9.)

## Byte divergence
`billing/reconcile.go` compares agent, relay and gateway byte counts; the relay can only prove a claim **too high** (its counter includes encryption overhead) and there is a 16 KiB absolute slack. See [billing](billing.md).

## Gotchas
- A task's exit mapping lives in `terminalState` and is mirrored by `billing.billable` (kept in sync by a test).
- Failed customer tasks still count as clean completions for the host (+0.01): the host did its job.
- Timing in tests uses `clock.Clock`/fake clock; do not use `time.Now` in scheduler logic.

## Starvation hold (v0.4.15)
`placement.go` `holdForStarvingTask`: for a node, `Store.OldestQueuedBefore(offer, now-StarveAfter)` finds the oldest queued task that fits the node's whole offer and has waited longer than `StarveAfter` (default 90 s). If it does not fit the node's free capacity right now, the claim filter gets `OlderThan = that task's created_at`, so only older tasks may be placed and the node drains until the big task fits (then it goes first). Nodes whose offer could never run it are unaffected. Tasks cooling down for the node (`cooldownTasks`) are passed as `ExcludeTaskIDs` so a rejected head task cannot stall the node. Tests: `fill_test.go` (integration).
