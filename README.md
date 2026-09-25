# LazyCake

A marketplace for leftover CPU: hosts rent out idle cores, customers run
batch containers on them. See `PLAN.md` for the design and `IMPLEMENTATION.md`
for the build order; `PROGRESS.md` tracks what's actually done.

## Trust score

Every node has a trust score in `[0, 1]`, starting at `0.5`
(`internal/coordinator/scheduler/trust.go`, `TrustTracker`). It's the
single number every fraud signal in the system feeds into, and the number
placement (task 5.4) and the dispatch loop itself read back out.

### Formula

The score is a running total, nudged by a fixed delta each time one of the
following happens, then clamped to `[0, 1]`:

| Event | Delta | Source |
|---|---|---|
| Clean completion (the host ran the task and reported honestly - any exit code) | `+0.01` | every `TaskSucceeded`/`TaskFailed` |
| Canary passed (verified by the platform's own gateway, not the agent) | `+0.02` | task 5.2 |
| Canary failed (expected canary never showed up, or the wrong result) | `-0.30` | task 5.2 |
| Byte divergence (agent/relay/gateway counts disagree by >2%) | `-0.20` | task 4.3 |
| Spec drift (measured task durations diverge from the node's own baseline) | `-0.15` | task 5.1 |
| Abandonment (the node vanished mid-task) | `-0.25` | task 3.4 |

Positive events are small and additive - trust is earned slowly, one clean
task at a time. Negative events are large - a single canary failure alone
takes a perfectly-trusted node from `1.0` to `0.7`, and from the `0.5`
starting point to `0.2`, the ban threshold, in one shot. This is
deliberate: false positives on the *positive* side are cheap (a slightly
too-generous score just means slightly more work sent to a node that
doesn't deserve it yet), but false negatives on the *negative* side are
expensive (a node that's actually lying keeps getting real customer
workloads). The asymmetry pushes the system to err toward suspicion.

### Consequences

- **Below `0.2`:** the node is banned - the dispatch loop
  (`scheduler.tick`) skips it entirely ("stop dispatching and freeze the
  balance" - IMPLEMENTATION.md task 5.3). No new tasks are ever placed on
  it again unless a human intervenes; there's no automatic path back from
  a ban in this codebase.
- **Scheduling priority (task 5.4):** trust score is one term in the
  placement score alongside cache locality and queue depth, so an honest,
  well-behaved node is preferred over a mediocre-but-not-banned one long
  before it would ever cross the ban line.
- **Canary rate (task 5.2):** trust score also sets how often a node gets a
  canary task - 5% of dispatches at low trust, 0.5% at high trust. A newly
  registered or already-suspicious node gets watched more closely; a node
  with a long clean history is checked less often, since checking has a
  real cost (it occupies a slot that could run real, revenue-generating
  work).

### Why these specific numbers

They're not derived from any external model - there's no historical fraud
data to calibrate against, since this is a from-scratch marketplace. The
design goals that shaped them:

1. A single canary failure or byte-divergence flag should be a serious
   event, not something a few clean tasks immediately erase.
2. An honest node should convergence toward `1.0` and stay there through
   ordinary operation, not hover near the starting point.
3. A node that's dishonest on even a modest fraction of its tasks (not
   every single one - an obvious cheat is a different, easier problem)
   should cross the ban threshold within a bounded number of tasks, not
   after months of activity.

`internal/coordinator/scheduler/trust_test.go`'s `TestTrustConvergence`
exercises both ends of this: a node that's honest throughout stays above
`0.8` after 50 tasks, and one that gets caught by a mix of the fraud
signals above on roughly a fifth of its tasks crosses the ban threshold
well within that same 50-task window.
