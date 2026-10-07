// Turning a machine's usage response into chart data (task 8.15).
import { fillBuckets, type StackPoint } from "./series";
import type { NodeUsage } from "./types";

/** The series key the server uses for every task beyond the biggest few. */
export const OTHERS_KEY = "_others";

export type UsageMetric = "cpu" | "memory" | "disk";

const WINDOW = { "24h": 24 * 3_600_000, "7d": 7 * 24 * 3_600_000 } as const;

/**
 * One chart point per period in the window. A period with no samples is a gap (the
 * machine was offline), never a zero, so a quiet machine and an absent one look different.
 */
export function usageStack(u: NodeUsage, metric: UsageMetric, now = Date.now()): StackPoint[] {
  const raw: StackPoint[] = u.series.map((p) => {
    const values: Record<string, number> = {};
    if (metric === "disk") {
      values.disk = p.disk_used_bytes;
    } else {
      for (const [task, share] of Object.entries(p.per_task)) {
        values[task] = metric === "cpu" ? share.cpu_cores : share.memory_bytes;
      }
      // Time with no task running is a real zero, not a gap: make sure the point exists.
    }
    return { at: p.at_ms, values };
  });
  return fillBuckets(raw, u.step === "hour" ? "hour" : "5m", WINDOW[u.range], now, true);
}

/** How much of the machine the tasks were using at a point, for the tooltip footer. */
export function usageAt(u: NodeUsage, at: number) {
  return u.series.find((p) => p.at_ms === at);
}

// Colour classes for the per-task bands: the first few tasks get distinct hues, "others" is neutral.
const BANDS = [
  "bg-accent",
  "bg-success",
  "bg-warning",
  "bg-info",
  "bg-danger",
  "bg-[rgb(var(--accent)/0.55)]",
  "bg-[rgb(var(--success)/0.55)]",
  "bg-[rgb(var(--info)/0.55)]",
];

export function bandClass(index: number): string {
  return BANDS[index % BANDS.length];
}

/** Percent 0..100 with no more precision than is meaningful. */
export function percent(fraction: number): string {
  const p = Math.max(0, fraction) * 100;
  return `${p < 10 ? Number(p.toFixed(1)) : Math.round(p)}%`;
}
