import { describe, expect, it } from "vitest";
import { MIN_COLUMN_MS, MIN_TRAFFIC_COLUMN_MS, bucketReadings, bucketTraffic, taskStack } from "./taskusage";
import type { TaskReading } from "./types";

const r = (at_ms: number, cpu: number, mem: number, out = 0, inn = 0): TaskReading => ({
  at_ms,
  cpu_cores: cpu,
  memory_bytes: mem,
  tunnel_out_bytes: out,
  tunnel_in_bytes: inn,
});

describe("bucketReadings", () => {
  it("averages CPU and memory but sums network, so totals survive the grouping", () => {
    const readings = [r(0, 0.4, 100, 10, 1), r(5_000, 0.6, 300, 20, 2), r(30_000, 1, 200, 5, 5)];
    const cols = bucketReadings(readings, 0, 30_000);
    expect(cols[0].n).toBe(2);
    expect(cols[0].cpu).toBeCloseTo(0.5);
    expect(cols[0].memory).toBe(200);
    expect(cols.reduce((n, c) => n + c.out, 0)).toBe(35);
    expect(cols.reduce((n, c) => n + c.in, 0)).toBe(8);
  });

  it("keeps a stretch with no readings as gap columns", () => {
    const cols = bucketReadings([r(0, 1, 1), r(10 * MIN_COLUMN_MS, 1, 1)], 0, 10 * MIN_COLUMN_MS);
    const stack = taskStack(cols, "cpu");
    expect(stack[0].gap).toBeUndefined();
    expect(stack[5].gap).toBe(true);
    expect(stack[stack.length - 1].gap).toBeUndefined();
  });

  it("never makes columns narrower than the reading interval, and caps how many there are", () => {
    const short = bucketReadings([r(0, 1, 1), r(15_000, 1, 1), r(30_000, 1, 1)], 0, 90_000);
    expect(short[1].at - short[0].at).toBe(MIN_COLUMN_MS);
    const day: TaskReading[] = Array.from({ length: 5000 }, (_, i) => r(i * 15_000, 1, 1));
    expect(bucketReadings(day, 0, 5000 * 15_000).length).toBeLessThanOrEqual(61);
  });

  it("includes a reading after the stated end instead of dropping it", () => {
    const cols = bucketReadings([r(0, 1, 1), r(100_000, 2, 1)], 0, 50_000);
    expect(cols[cols.length - 1].cpu).toBe(2);
  });

  it("returns nothing for no readings", () => {
    expect(bucketReadings([], 0, 1000)).toEqual([]);
  });
});

describe("taskStack", () => {
  it("shapes each metric for the chart", () => {
    const cols = bucketReadings([r(0, 0.5, 64, 9, 3)], 0, 0);
    expect(taskStack(cols, "cpu")[0].values).toEqual({ cpu: 0.5 });
    expect(taskStack(cols, "memory")[0].values).toEqual({ memory: 64 });
    expect(taskStack(cols, "network")[0].values).toEqual({ out: 9, in: 3 });
  });
});

describe("bucketTraffic", () => {
  const series = [
    { at_ms: 0, gateway_id: "gw_a", received: 10, sent: 100 },
    { at_ms: 0, gateway_id: "gw_b", received: 1, sent: 2 },
    { at_ms: 10_000, gateway_id: "gw_a", received: 10, sent: 100 },
    { at_ms: 40_000, gateway_id: "gw_b", received: 5, sent: 50 },
  ];

  it("keeps each gateway's own consumption separate, biggest first, with exact totals", () => {
    const { points, gateways, totals } = bucketTraffic(series, 0, 40_000);
    expect(gateways).toEqual(["gw_a", "gw_b"]);
    expect(totals.gw_a).toEqual({ received: 20, sent: 200 });
    expect(totals.gw_b).toEqual({ received: 6, sent: 52 });
    expect(points[0].values).toEqual({ gw_a: 110, gw_b: 3, received: 11, sent: 102 });
    expect(points.reduce((n, p) => n + p.values.gw_a, 0)).toBe(220);
    expect(points.reduce((n, p) => n + p.values.gw_b, 0)).toBe(58);
  });

  it("shows quiet moments as real zeros for every gateway, never as gaps", () => {
    const { points } = bucketTraffic(series, 0, 40_000);
    expect(points).toHaveLength(5);
    expect(points[2].values).toEqual({ gw_a: 0, gw_b: 0, received: 0, sent: 0 });
    expect(points[2].gap).toBeUndefined();
    expect(points[1].at - points[0].at).toBe(MIN_TRAFFIC_COLUMN_MS);
  });

  it("names traffic recorded before gateways were tracked, caps the columns, and handles no traffic", () => {
    const old = bucketTraffic([{ at_ms: 0, gateway_id: "", received: 1, sent: 1 }], 0, 0);
    expect(old.gateways).toEqual(["unknown"]);
    const day = Array.from({ length: 4000 }, (_, i) => ({ at_ms: i * 10_000, gateway_id: "g", received: 1, sent: 1 }));
    expect(bucketTraffic(day, 0, 4000 * 10_000).points.length).toBeLessThanOrEqual(61);
    expect(bucketTraffic([], 0, 1000).points).toEqual([]);
  });
});
