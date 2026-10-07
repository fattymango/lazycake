import { describe, expect, it } from "vitest";
import { OTHERS_KEY, percent, trimLeadingGap, usageStack } from "./usage";
import type { NodeUsage } from "./types";

const M5 = 300_000;
const base: NodeUsage = {
  supported: true,
  range: "24h",
  step: "5m",
  tasks: ["tsk_a"],
  series: [],
};

describe("usageStack", () => {
  const now = 1_000 * M5 + 7;

  it("starts at the first reading, with offline periods as gaps and idle periods as real zeros", () => {
    const u: NodeUsage = {
      ...base,
      series: [
        {
          at_ms: 998 * M5,
          samples: 20,
          host_cpu_busy: 0.1,
          host_mem_used_bytes: 1,
          disk_used_bytes: 5,
          tasks_cpu_cores: 0,
          tasks_memory_bytes: 0,
          tunnel_out_bytes: 0,
          tunnel_in_bytes: 0,
          per_task: {},
        },
        {
          at_ms: 1000 * M5,
          samples: 20,
          host_cpu_busy: 0.1,
          host_mem_used_bytes: 1,
          disk_used_bytes: 5,
          tasks_cpu_cores: 1,
          tasks_memory_bytes: 9,
          tunnel_out_bytes: 0,
          tunnel_in_bytes: 0,
          per_task: { tsk_a: { cpu_cores: 1, memory_bytes: 9, tunnel_out_bytes: 0, tunnel_in_bytes: 0 } },
        },
      ],
    };
    const pts = usageStack(u, "cpu", now);
    // The empty stretch before the first reading is not downtime, so the chart starts at the first reading.
    expect(pts[0].at).toBe(998 * M5);
    expect(pts).toHaveLength(1000 - 998 + 1);
    const at = (t: number) => pts.find((p) => p.at === t * M5)!;
    expect(at(1000).values).toEqual({ tsk_a: 1, _total: 1 });
    expect(at(998).gap).toBeUndefined(); // the machine reported, it was just idle
    expect(at(999).gap).toBe(true); // nothing reported
  });

  it("reads memory and disk from the right fields, and keeps the others band", () => {
    const u: NodeUsage = {
      ...base,
      series: [
        {
          at_ms: 1000 * M5,
          samples: 1,
          host_cpu_busy: 0,
          host_mem_used_bytes: 0,
          disk_used_bytes: 777,
          tasks_cpu_cores: 1,
          tasks_memory_bytes: 30,
          tunnel_out_bytes: 7,
          tunnel_in_bytes: 9,
          per_task: {
            tsk_a: { cpu_cores: 0.5, memory_bytes: 10, tunnel_out_bytes: 3, tunnel_in_bytes: 4 },
            [OTHERS_KEY]: { cpu_cores: 0.5, memory_bytes: 20, tunnel_out_bytes: 4, tunnel_in_bytes: 5 },
          },
        },
      ],
    };
    expect(usageStack(u, "memory", now).find((p) => p.at === 1000 * M5)!.values).toEqual({ tsk_a: 10, [OTHERS_KEY]: 20, _total: 30 });
    expect(usageStack(u, "network", now).find((p) => p.at === 1000 * M5)!.values).toEqual({ tsk_a: 7, [OTHERS_KEY]: 9, _total: 16 });
    expect(usageStack(u, "disk", now).find((p) => p.at === 1000 * M5)!.values).toEqual({ disk: 777 });
  });

  it("uses hourly buckets for a week and 30-second bins for an hour", () => {
    const hourly = usageStack({ ...base, range: "7d", step: "hour" }, "cpu", now);
    expect(hourly).toHaveLength(168); // no readings at all: every period is a gap, nothing to trim to
    expect(hourly[1].at - hourly[0].at).toBe(3_600_000);
    const fine = usageStack({ ...base, range: "1h", step: "30s" }, "cpu", now);
    expect(fine).toHaveLength(120);
    expect(fine[1].at - fine[0].at).toBe(30_000);
  });
});

describe("trimLeadingGap", () => {
  it("drops only the stretch before the first reading, keeping later downtime", () => {
    const g = (at: number) => ({ at, values: {}, gap: true });
    const d = (at: number) => ({ at, values: { a: 1 } });
    expect(trimLeadingGap([g(0), g(1), d(2), g(3), d(4)]).map((p) => p.at)).toEqual([2, 3, 4]);
    expect(trimLeadingGap([d(0), g(1)]).map((p) => p.at)).toEqual([0, 1]);
    expect(trimLeadingGap([g(0), g(1)]).map((p) => p.at)).toEqual([0, 1]);
  });
});

describe("percent", () => {
  it("keeps useful precision without false precision", () => {
    expect(percent(0.5)).toBe("50%");
    expect(percent(0.034)).toBe("3.4%");
    expect(percent(1.2)).toBe("120%");
    expect(percent(-1)).toBe("0%");
  });
});
