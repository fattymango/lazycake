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

export type TaskFilter = "all" | "active" | "succeeded" | "failed";

export function matchesTaskFilter(t: Task, f: TaskFilter): boolean {
  switch (f) {
    case "active":
      return !isTerminalState(t.state);
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
