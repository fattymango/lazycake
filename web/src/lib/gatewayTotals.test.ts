import { describe, expect, it } from "vitest";
import { gatewayTotals } from "./gatewayTotals";
import type { Gateway, Task } from "./types";

const gw = (id: string, connected: boolean, rx: number, tx: number, conns: number, services = 1): Gateway => ({
  id,
  label: id,
  connected,
  created_at_ms: 0,
  traffic: { received_from_tasks_bytes: rx, sent_to_tasks_bytes: tx, connections: conns },
  services: Array.from({ length: services }, (_, i) => ({ name: `s${i}`, port: 1000 + i })),
  version: "",
  latest_version: "",
  update_state: "unknown",
});
const task = (id: string, state: Task["state"], gateway?: string): Task => ({
  id,
  state,
  image: "x",
  cores: 1,
  memory_mb: 1,
  created_at_ms: 0,
  tunnel_targets: gateway ? [{ gateway_id: gateway, hostname: "db", port: 5432 }] : undefined,
});

describe("gatewayTotals", () => {
  const gateways = [gw("a", true, 100, 50, 3, 2), gw("b", false, 5, 5, 1), gw("c", true, 900, 100, 7)];
  const t = gatewayTotals(gateways, [
    task("t1", "running", "a"),
    task("t2", "queued", "c"),
    task("t3", "succeeded", "a"), // finished: not "using" a gateway now
    task("t4", "running"), // no gateway
    task("t5", "running", "gone"), // a gateway this account no longer has
  ]);

  it("adds up traffic, connections, services and what is connected", () => {
    expect(t.received).toBe(1005);
    expect(t.sent).toBe(155);
    expect(t.connections).toBe(11);
    expect(t.services).toBe(4);
    expect(t.connected).toBe(2);
  });

  it("counts only unfinished tasks that target one of these gateways", () => {
    expect(t.using).toBe(2);
  });

  it("ranks the gateways by total traffic, busiest first", () => {
    expect(t.ranked.map((r) => r.g.id)).toEqual(["c", "a", "b"]);
    expect(t.ranked[0].total).toBe(1000);
  });

  it("is all zeros for no gateways", () => {
    const e = gatewayTotals([], []);
    expect([e.received, e.sent, e.connections, e.using, e.connected, e.ranked.length]).toEqual([0, 0, 0, 0, 0, 0]);
  });
});
