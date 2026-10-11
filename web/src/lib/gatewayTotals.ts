import type { Gateway, Task } from "./types";

const ACTIVE = new Set<Task["state"]>(["queued", "reserved", "dispatched", "running"]);

/** The numbers that describe all of an account's gateways together, from what the gateway list already carries. */
export function gatewayTotals(gateways: Gateway[], tasks: Task[]) {
  const ids = new Set(gateways.map((g) => g.id));
  let received = 0;
  let sent = 0;
  let connections = 0;
  for (const g of gateways) {
    received += g.traffic.received_from_tasks_bytes;
    sent += g.traffic.sent_to_tasks_bytes;
    connections += g.traffic.connections;
  }
  const using = tasks.filter((t) => ACTIVE.has(t.state) && t.tunnel_targets?.some((x) => ids.has(x.gateway_id))).length;
  const ranked = [...gateways]
    .map((g) => ({ g, total: g.traffic.received_from_tasks_bytes + g.traffic.sent_to_tasks_bytes }))
    .sort((a, b) => b.total - a.total);
  return {
    connected: gateways.filter((g) => g.connected).length,
    services: gateways.reduce((n, g) => n + g.services.length, 0),
    received,
    sent,
    connections,
    using,
    ranked,
  };
}
