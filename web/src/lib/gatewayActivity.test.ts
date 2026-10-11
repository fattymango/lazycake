import { describe, expect, it } from "vitest";
import { gatewayActivity, lastTrafficAtMs } from "./gatewayActivity";
import type { Task } from "./types";

const task = (id: string, state: Task["state"], gw: string | null, created: number, started?: number): Task => ({
  id,
  state,
  image: "x",
  cores: 1,
  memory_mb: 1,
  created_at_ms: created,
  started_at_ms: started,
  tunnel_targets: gw ? [{ gateway_id: gw, hostname: "db", port: 5432 }] : undefined,
});

describe("gatewayActivity", () => {
  const tasks = [
    task("a", "running", "gw1", 100, 110),
    task("b", "queued", "gw1", 200),
    task("c", "dispatched", "gw1", 150),
    task("d", "succeeded", "gw1", 50, 60),
    task("e", "running", "gw2", 300, 310),
    task("f", "running", null, 400, 410),
  ];
  const a = gatewayActivity(tasks, "gw1");

  it("separates running from waiting and ignores other gateways and tasks without targets", () => {
    expect(a.running.map((t) => t.id)).toEqual(["a"]);
    expect(a.queued.map((t) => t.id)).toEqual(["b", "c"]);
  });

  it("finds the newest task by start time, falling back to submit time", () => {
    expect(a.lastTask?.id).toBe("b");
    expect(a.lastTaskAtMs).toBe(200);
  });

  it("is empty for a gateway nothing used", () => {
    const none = gatewayActivity(tasks, "gw9");
    expect(none.running).toEqual([]);
    expect(none.lastTask).toBeUndefined();
  });
});

describe("lastTrafficAtMs", () => {
  const p = (at_ms: number, r: number, s: number) => ({ at_ms, received_from_tasks_bytes: r, sent_to_tasks_bytes: s });
  it("skips empty buckets", () => {
    expect(lastTrafficAtMs([p(1, 5, 0), p(2, 0, 3), p(3, 0, 0)])).toBe(2);
    expect(lastTrafficAtMs([p(1, 0, 0)])).toBeUndefined();
    expect(lastTrafficAtMs(undefined)).toBeUndefined();
  });
});
