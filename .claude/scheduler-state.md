# Scheduler: state of a machine and of a task

Back to [README](README.md). Code: `internal/coordinator/scheduler`. Companion pages: [scheduler-trust](scheduler-trust.md) (rules and constants), [architecture](architecture.md) (task lifecycle end to end). This page answers one question: **"why is this task not running on that machine right now?"**

## 1. The one rule

Once a second, `tick` walks every **connected** node and calls `fillNode`. `fillNode` repeats `tryPlaceOne` (at most 16 per node per tick) until nothing more fits. `tryPlaceOne` asks the database for **one** queued task that fits, claims it atomically, and sends it. Everything below explains what makes a node or a task *ineligible* in that one pass. There is no other placement path (canaries excepted, section 4).

## 2. What the scheduler knows about a machine

| State | Where it lives | Set by | Effect |
|---|---|---|---|
| **Connected** | `nodes.connected` | agent stream connect/disconnect; cleared by a failed `Dispatch.Send` | not connected = skipped entirely |
| **Offer** | `nodes.offer_*` (cores, memory, disk) | the provider, validated by the agent | the ceiling; never exceeded |
| **Free capacity** | memory only: `Scheduler.freeCap[nodeID]` | agent `CapacityReport` (truth), minus every dispatch since (`reserveFree`) | the numbers a task must fit into. Before the first report: the whole offer |
| **Reserved** | `Store.NodeReservations` (sum over tasks in reserved/dispatched/running) | derived | what the dashboard shows as "Reserved by tasks"; what the scheduler counts, **not CPU actually burnt** |
| **Trust** | `TrustTracker` in memory, `nodes.trust_score` (loaded at startup) | task outcomes, canaries, drift | below 0.2 = **banned**: no dispatch at all, heals +0.2/h toward 0.5 |
| **Cooldown** | `Scheduler.rejected[(node,task)]` in memory, 30 s | the agent rejecting a task | that task is not offered to that node for 30 s |
| **Cold-pull slot** | `ColdPull` in memory | dispatching a task whose image the node lacks | at most 3 nodes pull the same digest at once |

A machine is therefore in one of these practical situations, in the order the scheduler checks them:

1. **Offline** - not connected. Its running tasks are reclaimed only after lease + margin (`reclaim.go`).
2. **Banned** - trust < 0.2. Logged once per transition (`noteBan`).
3. **Canary turn** - `maybeDispatchCanary` may send a test task instead of real work this tick.
4. **Full** - free cores/memory/disk smaller than every queued task. Looks idle on the CPU graph if the tasks are lazy: reserved is not used.
5. **Draining for a big task** (only one node per task; short tasks still backfill) - a queued task has waited more than `StarveAfter` (90 s) and fits this node's whole offer but not its free capacity: only *older* tasks may be placed here until it fits (section 3).
6. **Filling** - has room; gets tasks until nothing fits or 16 are placed.

## 3. What decides which queued task goes where

`ClaimQueuedTask` (SQL, `store/postgres_tasks.go`) takes the **oldest** queued task satisfying all of: arch matches, isolation supported, `cpu_cores <= free`, `memory_mb <= free`, `disk_mb <= free`, not in `ExcludeTaskIDs` (this node's cooldowns), and, when draining, `created_at < OlderThan`. Oldest-first plus a fit test means a stream of small tasks can overtake a big one forever; the **starvation hold** (`holdForStarvingTask`) is the fix:

- `Store.OldestQueuedBefore(offer, now - StarveAfter)` finds the oldest waiting task that would fit this node's *whole offer*.
- If it does not fit *free* capacity now, `filter.OlderThan = its created_at`. The node keeps finishing what it has and takes no newer work.
- A node whose offer can never run that task is unaffected, so a 4-core VM keeps working while an 8-core task waits for the PC.
- **Only one node drains per waiting task** (`Scheduler.drainers`, claim lapses after 5 s without renewal): the others keep working. Without this every node that could run the task emptied itself for it.
- **Backfill** (`backfillWindow`, `CapacityFilter.BackfillWallS`): a draining node still takes newer tasks whose `wall_timeout_s` is at most the time until it would have room for the head if every running task ran to its own wall limit, so they cannot delay it. No running tasks known, or no way to fit the head: plain hold.

After a claim: cooldown re-check, cold-pull cap, balance hold (`PlaceHold`), build and send `Dispatch`, state `dispatched`, then `reserveFree` subtracts the task from `freeCap`. Any failure releases the task back to `queued` (`ReleaseReservedTask`); a failed send also marks the node disconnected.

## 4. Task states as the scheduler sees them

`queued` -> `reserved` (claimed inside the SQL, not yet sent) -> `dispatched` (sent) -> `running` (agent `TaskStarted`) -> `succeeded` / `failed` / `fenced` / `abandoned` / `cancelled`. Capacity-holding states: reserved, dispatched, running. Agent rejection (`OnTaskRejected`) puts it back to `queued` and starts the 30 s cooldown. Canaries are real `Task` rows built by `canary.go` with probability by trust (5% low to 0.5% high); they are dispatched instead of real work on that tick.

## 5. Debugging "task X is queued but machine Y has capacity"

1. Is Y connected, and not banned? (`nodes.connected`, trust in the dashboard, log "not dispatching to this node").
2. Does X fit Y's **free** numbers (offer minus reserved), not its CPU graph? Dashboard: "Reserved by tasks".
3. Is X older than 90 s and bigger than what is free? Then Y is intentionally draining for the oldest big task, maybe not X.
4. Did Y reject X in the last 30 s? (log "task rejected").
5. Cold-pull cap reached for X's image? (log "deferring dispatch").
6. Balance: `PlaceHold` failing releases the task silently-ish (log "placing balance hold, requeueing").

## 6. Scaling notes (what to change, not guess)

- The loop is **single-threaded and in-memory** (`freeCap`, `rejected`, `ColdPull` live in one coordinator process). It is right for one coordinator; a second coordinator needs these in the database or a leader election first.
- Cost per tick is one `ListNodes` plus, per connected node, one claim query (plus one `OldestQueuedBefore` while tasks are queued). That is fine for hundreds of nodes on the small box; with thousands, batch the claims or index `tasks(state, created_at)` and add a per-node "nothing fits" memo.
- New eligibility rules belong in `CapacityFilter` (so SQL stays the single source of "what fits") and are added to `tryPlaceOne`'s filter, with a test in `fill_test.go` using `starvationEnv`/`dispatchRecorder`.
- Tests that must keep failing without their fix: drain-one-node (`TestOnlyOneNodeDrainsForAWaitingBigTask`), backfill (`TestADrainingNodeStillRunsShortTasks...`), fill (`TestTickFillsANodeToItsCapacityAndNoFurther`), starvation (`TestAWaitingBigTaskHoldsTheNodeBackAndGoesFirstWhenItFits`), cooldown (`TestATaskCoolingDownForThisNodeDoesNotStallIt`), trust (`TestTrustSurvivesARestart`).
