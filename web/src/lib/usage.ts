// Turning a machine's usage response into chart data (task 8.15).
import { fillBuckets, type StackPoint } from "./series";
import type { NodeUsage } from "./types";

/** The series key of the line that adds every task together. */
export const TOTAL_KEY = "_total";

/** The series key the server uses for every task beyond the biggest few. */
export const OTHERS_KEY = "_others";

export type UsageMetric = "cpu" | "memory" | "disk" | "network";

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
        values[task] =
          metric === "cpu" ? share.cpu_cores : metric === "memory" ? share.memory_bytes : share.tunnel_out_bytes + share.tunnel_in_bytes;
      }
      // Time with no task running is a real zero, not a gap: the point exists with a total of 0.
      values[TOTAL_KEY] = Object.values(values).reduce((a, b) => a + b, 0);
    }
    return { at: p.at_ms, values };
  });
  return fillBuckets(raw, u.step === "hour" ? "hour" : "5m", WINDOW[u.range], now, true);
}

/** How much of the machine the tasks were using at a point, for the tooltip footer. */
export function usageAt(u: NodeUsage, at: number) {
  return u.series.find((p) => p.at_ms === at);
}

// Line colours for the per-task series: the first few tasks get distinct hues; "others" is neutral.
export const SERIES_COLORS = [
  "rgb(var(--accent))",
  "rgb(var(--success))",
  "rgb(var(--warning))",
  "rgb(var(--info))",
  "rgb(var(--danger))",
  "rgb(var(--accent) / 0.55)",
  "rgb(var(--success) / 0.55)",
  "rgb(var(--info) / 0.55)",
];
export const OTHERS_COLOR = "rgb(var(--muted))";

export function seriesColor(index: number): string {
  return SERIES_COLORS[index % SERIES_COLORS.length];
}

/** Percent 0..100 with no more precision than is meaningful. */
export function percent(fraction: number): string {
  const p = Math.max(0, fraction) * 100;
  return `${p < 10 ? Number(p.toFixed(1)) : Math.round(p)}%`;
}
