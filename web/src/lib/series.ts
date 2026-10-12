// Time-series helpers shared by every usage chart.

export interface StackPoint {
  /** Start of the bucket, epoch ms. */
  at: number;
  /** Value per series key; a key that is absent means zero. */
  values: Record<string, number>;
  /** No data was received for this period (e.g. the machine was offline): drawn as a gap, not as zero. */
  gap?: boolean;
}

const STEP_MS = { "10s": 10_000, "30s": 30_000, "5m": 300_000, hour: 3_600_000, day: 86_400_000 } as const;
export type Step = keyof typeof STEP_MS;

/**
 * The server only returns buckets that had traffic. A chart that drew just those
 * would squash a quiet week into a few bars, so fill every bucket of the window
 * (oldest first, ending with the bucket that contains `now`) with zeros. With `markGaps`
 * the filled buckets are flagged as having no data, for charts where "nothing reported"
 * (the machine was offline) differs from "zero".
 */
export function fillBuckets(points: StackPoint[], step: Step, windowMs: number, now = Date.now(), markGaps = false): StackPoint[] {
  const ms = STEP_MS[step];
  const last = Math.floor(now / ms) * ms;
  const first = Math.floor((now - windowMs) / ms) * ms + ms;
  const byBucket = new Map<number, StackPoint>();
  for (const p of points) {
    const b = Math.floor(p.at / ms) * ms;
    const cur = byBucket.get(b);
    if (!cur) {
      byBucket.set(b, { at: b, values: { ...p.values } });
    } else {
      for (const [k, v] of Object.entries(p.values)) cur.values[k] = (cur.values[k] ?? 0) + v;
    }
  }
  const out: StackPoint[] = [];
  for (let t = first; t <= last; t += ms) out.push(byBucket.get(t) ?? { at: t, values: {}, ...(markGaps ? { gap: true } : {}) });
  return out;
}

export function stackTotal(p: StackPoint): number {
  return Object.values(p.values).reduce((a, b) => a + b, 0);
}

/**
 * A y-axis that fits the data: a round step (1, 2, 2.5 or 5 times a power of ten) giving about four intervals, and a top just above `max` (a few
 * percent of headroom so the line never sits on the edge). A fixed 0, half, top axis made 2 cores of usage look flat once an 8-core limit
 * stretched the scale to 10; this keeps the axis as tight as readable numbers allow.
 */
export function niceScale(max: number): { top: number; step: number; ticks: number[] } {
  if (!(max > 0)) return { top: 1, step: 0.25, ticks: [0, 0.25, 0.5, 0.75, 1] };
  const raw = (max * 1.04) / 4;
  const exp = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = ([1, 2, 2.5, 5, 10].map((m) => m * exp).find((s) => s >= raw) ?? 10 * exp) as number;
  const count = Math.max(1, Math.ceil((max * 1.04) / step - 1e-9));
  const clean = (v: number) => Number(v.toPrecision(12));
  return { top: clean(count * step), step, ticks: Array.from({ length: count + 1 }, (_, i) => clean(i * step)) };
}
