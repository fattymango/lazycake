import { describe, expect, it } from "vitest";
import { secondsLeft } from "./throttle";

describe("secondsLeft", () => {
  it("counts down in whole seconds, rounding up so it never shows 0 while still locked", () => {
    expect(secondsLeft(10_000, 0)).toBe(10);
    expect(secondsLeft(10_000, 9_001)).toBe(1);
    expect(secondsLeft(10_000, 9_999)).toBe(1);
  });
  it("is zero once the time has passed", () => {
    expect(secondsLeft(10_000, 10_000)).toBe(0);
    expect(secondsLeft(10_000, 50_000)).toBe(0);
    expect(secondsLeft(0, Date.now())).toBe(0);
  });
});
