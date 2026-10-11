import { describe, expect, it } from "vitest";
import { bitrate } from "./bitrate";

describe("bitrate", () => {
  it("keeps three significant digits in the unit that fits", () => {
    expect(bitrate(0)).toBe("0 bps");
    expect(bitrate(850)).toBe("850 bps");
    expect(bitrate(2_400)).toBe("2.4 kbps");
    expect(bitrate(850_000)).toBe("850 kbps");
    expect(bitrate(2_666_666)).toBe("2.67 Mbps");
    expect(bitrate(72_000_000)).toBe("72 Mbps");
    expect(bitrate(1_200_000_000)).toBe("1.2 Gbps");
  });
  it("does not hide a tiny real value as zero", () => {
    expect(bitrate(1)).toBe("1 bps");
    expect(bitrate(17)).toBe("17 bps");
  });
  it("moves up a unit instead of printing 1000", () => {
    expect(bitrate(999_600)).toBe("1 Mbps");
    expect(bitrate(999_999_999)).toBe("1 Gbps");
  });
  it("says dash for something that was never measured", () => {
    expect(bitrate(null)).toBe("—");
    expect(bitrate(undefined)).toBe("—");
    expect(bitrate(Number.NaN)).toBe("—");
  });
});
