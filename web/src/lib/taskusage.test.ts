import { describe, expect, it } from "vitest";
import { MIN_COLUMN_MS, bucketReadings, taskStack } from "./taskusage";
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
