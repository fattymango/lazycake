import type { LedgerEntry, Task } from "./types";
import { isTerminalState } from "@/ui/status";

/** Wall-clock run time: since start while running, start->finish once done. */
export function taskDurationMs(t: Task, now = Date.now()): number | undefined {
  if (!t.started_at_ms) return undefined;
  const end = t.finished_at_ms ?? (isTerminalState(t.state) ? undefined : now);
  return end === undefined ? undefined : Math.max(0, end - t.started_at_ms);
}

/**
 * What a task has been charged, summed per task from the ledger, as a positive
 * amount. The ledger stores charges as negative amounts (credits positive), so
 * the sign is dropped here: callers show it as a cost.
 */
export function costByTask(entries: LedgerEntry[] | undefined): Map<string, number> {
  const m = new Map<string, number>();
  for (const e of entries ?? []) {
    if (e.kind === "charge") m.set(e.task_id, (m.get(e.task_id) ?? 0) + Math.abs(e.amount_micros));
  }
  return m;
}

export type TaskFilter = "all" | "queued" | "running" | "succeeded" | "failed" | "stopped";

export function matchesTaskFilter(t: Task, f: TaskFilter): boolean {
  switch (f) {
    case "queued":
      return t.state === "queued" || t.state === "reserved";
    case "running":
      return t.state === "dispatched" || t.state === "running";
    case "stopped":
      return t.state === "cancelled";
    case "succeeded":
      return t.state === "succeeded";
    case "failed":
      return t.state === "failed" || t.state === "fenced" || t.state === "abandoned";
    default:
      return true;
  }
}

/** Plain-text form of what the task runs, for display and copying. */
export function taskCommand(t: Task): string | null {
  const parts = [...(t.entrypoint ?? []), ...(t.args ?? [])];
  return parts.length ? parts.map((p) => (/\s/.test(p) ? JSON.stringify(p) : p)).join(" ") : null;
}

export type TaskSort = "newest" | "oldest" | "longest" | "cost";

/** A sorted copy. Tasks with no value for the key (not started, never charged) go last. */
export function sortTasks(tasks: Task[], sort: TaskSort, costs: Map<string, number>, now = Date.now()): Task[] {
  const byKey = (key: (t: Task) => number | undefined) => (a: Task, b: Task) => (key(b) ?? -1) - (key(a) ?? -1);
  const out = [...tasks];
  switch (sort) {
    case "oldest":
      return out.sort((a, b) => a.created_at_ms - b.created_at_ms);
    case "longest":
      return out.sort(byKey((t) => taskDurationMs(t, now)));
    case "cost":
      return out.sort(byKey((t) => costs.get(t.id)));
    default:
      return out.sort((a, b) => b.created_at_ms - a.created_at_ms);
  }
}
