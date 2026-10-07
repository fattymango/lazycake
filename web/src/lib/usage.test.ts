import { describe, expect, it } from "vitest";
import { OTHERS_KEY, percent, usageStack } from "./usage";
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

  it("covers the whole window, with offline periods as gaps and idle periods as real zeros", () => {
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
          per_task: { tsk_a: { cpu_cores: 1, memory_bytes: 9 } },
        },
      ],
    };
    const pts = usageStack(u, "cpu", now);
    expect(pts).toHaveLength(288);
    const at = (t: number) => pts.find((p) => p.at === t * M5)!;
    expect(at(1000).values).toEqual({ tsk_a: 1 });
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
          per_task: { tsk_a: { cpu_cores: 0.5, memory_bytes: 10 }, [OTHERS_KEY]: { cpu_cores: 0.5, memory_bytes: 20 } },
        },
      ],
    };
    expect(usageStack(u, "memory", now).find((p) => p.at === 1000 * M5)!.values).toEqual({ tsk_a: 10, [OTHERS_KEY]: 20 });
    expect(usageStack(u, "disk", now).find((p) => p.at === 1000 * M5)!.values).toEqual({ disk: 777 });
  });

  it("uses hourly buckets for a week", () => {
    const pts = usageStack({ ...base, range: "7d", step: "hour" }, "cpu", now);
    expect(pts).toHaveLength(168);
    expect(pts[1].at - pts[0].at).toBe(3_600_000);
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
