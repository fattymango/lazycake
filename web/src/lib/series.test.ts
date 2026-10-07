import { describe, expect, it } from "vitest";
import { fillBuckets, niceMax, stackTotal } from "./series";

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

describe("niceMax", () => {
  it("rounds up to a readable axis top", () => {
    expect(niceMax(0)).toBe(1);
    expect(niceMax(3.2)).toBe(5);
    expect(niceMax(7)).toBe(10);
    expect(niceMax(1234)).toBe(2000);
    expect(niceMax(100)).toBe(100);
  });
});
