// Time-series helpers shared by every usage chart.

export interface StackPoint {
  /** Start of the bucket, epoch ms. */
  at: number;
  /** Value per series key; a key that is absent means zero. */
  values: Record<string, number>;
}

const STEP_MS = { hour: 3_600_000, day: 86_400_000 } as const;
export type Step = keyof typeof STEP_MS;

/**
 * The server only returns buckets that had traffic. A chart that drew just those
 * would squash a quiet week into a few bars, so fill every bucket of the window
 * (oldest first, ending with the bucket that contains `now`) with zeros.
 */
export function fillBuckets(points: StackPoint[], step: Step, windowMs: number, now = Date.now()): StackPoint[] {
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
  for (let t = first; t <= last; t += ms) out.push(byBucket.get(t) ?? { at: t, values: {} });
  return out;
}

export function stackTotal(p: StackPoint): number {
  return Object.values(p.values).reduce((a, b) => a + b, 0);
}

/** A y-axis maximum that is a round number above `max`, so the top gridline reads cleanly. */
export function niceMax(max: number): number {
  if (max <= 0) return 1;
  const exp = Math.pow(10, Math.floor(Math.log10(max)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * exp >= max) return m * exp;
  return 10 * exp;
}
