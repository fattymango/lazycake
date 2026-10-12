import { describe, expect, it } from "vitest";
import { fillBuckets, niceScale, stackTotal } from "./series";

const H = 3_600_000;

describe("fillBuckets", () => {
  const now = 100 * H + 1234;

  it("fills quiet hours with zeros so the window is always full", () => {
    const out = fillBuckets([{ at: 98 * H, values: { a: 5 } }], "hour", 4 * H, now);
    expect(out.map((p) => p.at / H)).toEqual([97, 98, 99, 100]);
    expect(out.map(stackTotal)).toEqual([0, 5, 0, 0]);
  });

  it("merges points that land in the same bucket instead of dropping one", () => {
    const out = fillBuckets(
      [
        { at: 99 * H + 10, values: { a: 1, b: 2 } },
        { at: 99 * H + 20, values: { a: 4 } },
      ],
      "hour",
      2 * H,
      now
    );
    expect(out.find((p) => p.at === 99 * H)?.values).toEqual({ a: 5, b: 2 });
  });

  it("flags buckets with no data as gaps only when asked, and never flags real data", () => {
    const real = { at: 98 * H, values: { a: 0 } }; // a real reading of zero is data, not a gap
    const plain = fillBuckets([real], "hour", 3 * H, now);
    expect(plain.some((p) => p.gap)).toBe(false);
    const marked = fillBuckets([real], "hour", 3 * H, now, true);
    expect(marked.find((p) => p.at === 98 * H)?.gap).toBeUndefined();
    expect(marked.filter((p) => p.gap)).toHaveLength(marked.length - 1);
  });

  it("buckets by five minutes", () => {
    const out = fillBuckets([], "5m", 20 * 60_000, 1_000_000_000);
    expect(out).toHaveLength(4);
    expect(out[1].at - out[0].at).toBe(300_000);
  });

  it("ignores points outside the window", () => {
    const out = fillBuckets([{ at: 10 * H, values: { a: 9 } }], "hour", 3 * H, now);
    expect(out.reduce((n, p) => n + stackTotal(p), 0)).toBe(0);
  });
});

describe("niceScale", () => {
  it("fits the data with round steps and a little headroom", () => {
    expect(niceScale(0)).toMatchObject({ top: 1, step: 0.25 });
    expect(niceScale(2.1).ticks).toEqual([0, 1, 2, 3]);
    expect(niceScale(0.9).ticks).toEqual([0, 0.25, 0.5, 0.75, 1]);
    expect(niceScale(8).ticks).toEqual([0, 2.5, 5, 7.5, 10]);
    expect(niceScale(1234).top).toBe(1500);
  });
  it("always has a top above the data and a sensible number of gridlines", () => {
    for (const v of [0.0017, 0.3, 1, 1.99, 2, 3.2, 7, 12, 99, 100, 1234, 5e9]) {
      const s = niceScale(v);
      expect(s.top).toBeGreaterThan(v);
      expect(s.ticks.length).toBeGreaterThanOrEqual(3);
      expect(s.ticks.length).toBeLessThanOrEqual(7);
      expect(s.ticks[s.ticks.length - 1]).toBe(s.top);
    }
  });
});
