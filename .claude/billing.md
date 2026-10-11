# Billing, pricing and holds (`internal/coordinator/billing`, `pricing`)

Back to [README](README.md). Money is integer **micros** (1,000,000 = 1 unit; the dashboard formats it).

## Price (`pricing.Price`)
`price = base_fee + (cpu_rate·cores + ram_rate·GB + disk_rate·GB) · normalised_s + net_rate·GB_transferred + cold_start_fee`. `DefaultRates` (placeholders, a business decision): base 100, CPU 50/core-s, RAM 10/GB-s, disk 2/GB-s, net 1,000/GB, cold start 5,000. `normalised_s` = wall duration × the node's benchmark score (1.0 if never benchmarked), so a fast machine does not overcharge. `pricing.WorstCase` bounds the pre-submission affordability check: full `wall_timeout_s`, score 1.0, the task's egress cap, cold pull assumed. It lives in its own package so `api` and `billing` share the exact formula without importing each other.

## Holds
At dispatch the scheduler calls `PlaceHold(task, account, amount)` (table `task_holds`, one per task). Available balance = `accounts.balance_micros` − sum of the account's holds. A hold is released when the task settles **or** when a task finishes without ever having started (no meter row: bad image, rejection) — `Meters.OnTaskFinished` does this on `store.ErrNotFound` from `GetMeter`. An abandoned task (reclaimed after a missed lease) never reaches `OnTaskFinished`, so `reclaimOverdue` releases its hold and applies the trust penalty itself.

## Meters (`meters.go`)
`OnTaskStarted` → `RecordMeterStarted`. `OnTaskFinished(nodeID, event)`: feeds the reconciler (`RecordAgentBytes`, `Finalize`), reads the meter, computes `duration_s` (coordinator's own start/finish clock) and `normalised_s`, `RecordMeterFinished`, then `Ledger.Settle`.

## Ledger (`ledger.go`)
`Settle` prices and, if `billable(exitReason)`, writes ledger entries (customer debit, provider credit) and releases the hold. Not billable: `fenced`, `cancelled` (coordinator moved on). Billable: `exited` (any code), `stopped` (customer-requested stop), `error`, `wall_timeout`, `oom`, `egress_exceeded`. Abandoned tasks never reach `Settle`. Network is billed from the agent's byte count (available immediately), not from the reconciliation result.

## Byte reconciliation (`reconcile.go`)
Three independent counts per task: agent, relay, gateway. `Reconciler` accumulates, and 10 s (`finalizeGrace`) after finish computes divergence = spread / largest. Only agent vs gateway disagreement beyond 16 KiB is held against the agent; the relay can only show a claim too high. A flagged task lowers the node's trust (−0.20). The agent must send `bytes_sent/bytes_recv` in `TaskFinished` (a missing report once made every tunnel task look 100% divergent).

## Caveats
- Customers add balance through `POST /api/portal/customer/balance/add` (demo-style top up; there is no payment processor).
- Do not delete `task_holds` rows by hand unless the task is finished and has no meter row (v0.4.0 cleanup did exactly this for 34 rows, with a CSV backup first).
