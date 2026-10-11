import type { GatewayTraffic, Task } from "./types";

export interface GatewayActivity {
  /** Started and not finished: the container is up. */
  running: Task[];
  /** Waiting for a machine or on its way to one (queued, reserved, dispatched). */
  queued: Task[];
  /** The newest task that asked for this gateway, any state. */
  lastTask?: Task;
  /** Start time of that task, or its submit time if it never started. */
  lastTaskAtMs?: number;
}

const WAITING = new Set<Task["state"]>(["queued", "reserved", "dispatched"]);

/** What tasks are doing with one gateway, from the account's recent tasks (the list is capped, newest first). */
export function gatewayActivity(tasks: Task[], gatewayId: string): GatewayActivity {
  const mine = tasks.filter((t) => t.tunnel_targets?.some((x) => x.gateway_id === gatewayId));
  const at = (t: Task) => t.started_at_ms ?? t.created_at_ms;
  const lastTask = mine.reduce<Task | undefined>((best, t) => (!best || at(t) > at(best) ? t : best), undefined);
  return {
    running: mine.filter((t) => t.state === "running"),
    queued: mine.filter((t) => WAITING.has(t.state)),
    lastTask,
    lastTaskAtMs: lastTask && at(lastTask),
  };
}

/** The newest traffic bucket with bytes in it: when a task last actually moved data through the gateway. */
export function lastTrafficAtMs(series: GatewayTraffic["series"] | undefined): number | undefined {
  let last: number | undefined;
  for (const p of series ?? []) {
    if (p.received_from_tasks_bytes + p.sent_to_tasks_bytes > 0 && (last === undefined || p.at_ms > last)) last = p.at_ms;
  }
  return last;
}
