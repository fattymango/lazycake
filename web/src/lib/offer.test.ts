import { describe, expect, it } from "vitest";
import {
  HARD_LIMITS,
  SLIDER_STOPS,
  bigOffers,
  defaultOffer,
  maxOffer,
  offerProblems,
  offerRequest,
  parseCapacity,
  parseWhole,
  sliderPosition,
  snapToStop,
  stopsFor,
} from "./offer";

const caps = { cores: 12, memoryMB: 15314, diskMB: 441802, networkMbps: 1000 };

describe("parseWhole: what a person types becomes a safe whole number", () => {
  it("keeps only digits: no decimals, signs, exponents or spaces", () => {
    expect(parseWhole("8", "cores")).toBe(8);
    expect(parseWhole("2.5", "cores")).toBe(25); // the dot is dropped, never a fraction
    expect(parseWhole("-4", "cores")).toBe(4);
    expect(parseWhole("1e9", "cores")).toBe(19);
    expect(parseWhole(" 1 6 ", "cores")).toBe(16);
    expect(parseWhole("007", "cores")).toBe(7);
  });
  it("is empty (NaN), not zero, when nothing numeric is typed", () => {
    expect(parseWhole("", "cores")).toBeNaN();
    expect(parseWhole("abc", "cores")).toBeNaN();
  });
  it("can never exceed the field's hard ceiling, however long the input", () => {
    expect(parseWhole("999999999", "cores")).toBe(HARD_LIMITS.cores.max);
    expect(parseWhole("9".repeat(5000), "memoryGB")).toBe(HARD_LIMITS.memoryGB.max);
    expect(parseWhole("99999999999999999999999999999", "networkMbps")).toBe(HARD_LIMITS.networkMbps.max);
    // Ceilings are exactly reachable.
    expect(parseWhole(String(HARD_LIMITS.storageGB.max), "storageGB")).toBe(HARD_LIMITS.storageGB.max);
  });
  it("no ceiling can overflow a 32-bit megabyte field once converted", () => {
    expect(HARD_LIMITS.memoryGB.max * 1024).toBeLessThan(2 ** 31);
    expect(HARD_LIMITS.storageGB.max * 1024).toBeLessThan(2 ** 31);
  });
});

describe("parseCapacity", () => {
  it("reads the line `agent capacity` prints", () => {
    expect(parseCapacity("cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000")).toEqual(caps);
    expect(parseCapacity("noise before\ncores=4 memory_mb=8000 disk_mb=100000 network_mbps=0\n")).toEqual({
      cores: 4,
      memoryMB: 8000,
      diskMB: 100000,
      networkMbps: null,
    });
  });
  it("refuses anything that isn't that line", () => {
    expect(parseCapacity("")).toBeNull();
    expect(parseCapacity("cores=12")).toBeNull();
    expect(parseCapacity("cores=0 memory_mb=1 disk_mb=1")).toBeNull();
    expect(parseCapacity("xcores=12 memory_mb=15314 disk_mb=441802")).toBeNull();
  });
});

describe("offerProblems", () => {
  const ok = { cores: 2, memoryGB: 4, storageGB: 50, networkMbps: 100 };

  it("accepts a sensible offer, and exactly the whole numbers the machine has", () => {
    expect(offerProblems(ok, caps)).toEqual({});
    expect(offerProblems({ cores: 12, memoryGB: 14, storageGB: 431, networkMbps: 1000 }, caps)).toEqual({});
  });

  it("names every value that is bigger than the machine, in plain words", () => {
    const p = offerProblems({ cores: 13, memoryGB: 15, storageGB: 500, networkMbps: 2000 }, caps);
    expect(p.cores).toBe("This machine only has 12 cores.");
    expect(p.memoryGB).toBe("This machine only has 14 GB.");
    expect(p.storageGB).toBe("This machine only has 431 GB free.");
    expect(p.networkMbps).toBe("This machine's network link is 1,000 Mbps.");
  });

  it("allows going past the slider's top in the field, up to the machine or the hard ceiling", () => {
    const bigMachine = { cores: 64, memoryMB: 256 * 1024, diskMB: 4 * 1024 * 1024, networkMbps: 10000 };
    expect(offerProblems({ cores: 48, memoryGB: 128, storageGB: 2000, networkMbps: 5000 }, bigMachine)).toEqual({});
    expect(offerProblems({ cores: 48, memoryGB: 128, storageGB: 2000, networkMbps: 5000 }, null)).toEqual({});
  });

  it("rejects fractions, empties and out-of-range values", () => {
    expect(offerProblems({ ...ok, cores: 2.5 }, null).cores).toBeDefined();
    expect(offerProblems({ ...ok, cores: Number.NaN }, null).cores).toBeDefined();
    expect(offerProblems({ ...ok, memoryGB: 0 }, null).memoryGB).toBeDefined();
    expect(offerProblems({ ...ok, networkMbps: HARD_LIMITS.networkMbps.max + 1 }, null).networkMbps).toMatch(/At most/);
  });

  it("can't check the network against a link whose speed is unknown", () => {
    expect(offerProblems({ ...ok, networkMbps: 5000 }, { ...caps, networkMbps: null })).toEqual({});
  });
});

describe("bigOffers: when the provider will feel it", () => {
  it("flags absolute big amounts when the machine isn't known", () => {
    expect(bigOffers({ cores: 2, memoryGB: 4, storageGB: 20, networkMbps: 100 }, null)).toEqual([]);
    expect(bigOffers({ cores: 8, memoryGB: 16, storageGB: 50, networkMbps: 500 }, null)).toEqual([
      "cores",
      "memoryGB",
      "storageGB",
      "networkMbps",
    ]);
    expect(bigOffers({ cores: 16, memoryGB: 4, storageGB: 20, networkMbps: 100 }, null)).toEqual(["cores"]);
  });
  it("flags half the machine or more when it is known", () => {
    expect(bigOffers({ cores: 5, memoryGB: 4, storageGB: 20, networkMbps: 100 }, caps)).toEqual([]);
    expect(bigOffers({ cores: 6, memoryGB: 7, storageGB: 20, networkMbps: 100 }, caps)).toEqual(["cores", "memoryGB"]);
  });
  it("ignores an empty or broken field", () => {
    expect(bigOffers({ cores: Number.NaN, memoryGB: 0, storageGB: 20, networkMbps: 100 }, null)).toEqual([]);
  });
});

describe("defaults, request and slider", () => {
  it("never default above what the machine has, and always whole", () => {
    expect(defaultOffer(null)).toEqual({ cores: 2, memoryGB: 2, storageGB: 10, networkMbps: 100 });
    const tiny = { cores: 1, memoryMB: 1536, diskMB: 4096, networkMbps: 50 };
    const d = defaultOffer(tiny);
    expect(d).toEqual({ cores: 1, memoryGB: 1, storageGB: 4, networkMbps: 50 });
    expect(offerProblems(d, tiny)).toEqual({});
    expect(maxOffer(tiny).memoryGB).toBe(1);
  });

  it("sends whole MB (a GB is 1024 MB)", () => {
    expect(offerRequest({ cores: 6, memoryGB: 12, storageGB: 200, networkMbps: 940 })).toEqual({
      cores: 6,
      memory_mb: 12288,
      disk_mb: 204800,
      network_mbps: 940,
    });
  });

  it("slider stops follow the plan: powers of two, capped at 24 cores, 64 GB memory, 100 GB storage", () => {
    expect(SLIDER_STOPS.cores).toEqual([1, 2, 4, 8, 16, 24]);
    expect(SLIDER_STOPS.memoryGB[SLIDER_STOPS.memoryGB.length - 1]).toBe(64);
    expect(SLIDER_STOPS.storageGB[SLIDER_STOPS.storageGB.length - 1]).toBe(100);
    for (const stops of Object.values(SLIDER_STOPS)) expect([...stops].sort((a, b) => a - b)).toEqual(stops);
  });

  it("the thumb sits between checkpoints for an in-between value, and stays at the end beyond the top", () => {
    const stops = SLIDER_STOPS.cores;
    expect(sliderPosition(stops, 1)).toBe(0);
    expect(sliderPosition(stops, 8)).toBe(3);
    expect(sliderPosition(stops, 12)).toBe(3.5);
    expect(sliderPosition(stops, 24)).toBe(5);
    expect(sliderPosition(stops, 48)).toBe(5); // typed past the slider's top
    expect(sliderPosition(stops, Number.NaN)).toBe(0);
  });

  it("dragging snaps to the nearest checkpoint", () => {
    expect(snapToStop(SLIDER_STOPS.cores, 2.4)).toBe(4);
    expect(snapToStop(SLIDER_STOPS.cores, 2.6)).toBe(8);
    expect(snapToStop(SLIDER_STOPS.cores, -3)).toBe(1);
    expect(snapToStop(SLIDER_STOPS.cores, 99)).toBe(24);
  });
});

describe("stopsFor: the slider never offers more than the machine has, and can reach exactly what it has", () => {
  it("is the usual checkpoints when the machine is unknown or bigger than the slider", () => {
    expect(stopsFor("cores", null)).toEqual([1, 2, 4, 8, 16, 24]);
    expect(stopsFor("cores", 64)).toEqual([1, 2, 4, 8, 16, 24]);
    expect(stopsFor("cores", 24)).toEqual([1, 2, 4, 8, 16, 24]);
  });
  it("ends at the machine's own amount when it is smaller", () => {
    expect(stopsFor("cores", 12)).toEqual([1, 2, 4, 8, 12]);
    expect(stopsFor("memoryGB", 14)).toEqual([1, 2, 4, 8, 14]);
    expect(stopsFor("storageGB", 431)).toEqual([5, 10, 20, 50, 100]);
    expect(stopsFor("networkMbps", 100)).toEqual([10, 25, 50, 100]);
  });
  it("always has at least two checkpoints, even on a tiny machine", () => {
    expect(stopsFor("cores", 1).length).toBeGreaterThanOrEqual(2);
    expect(stopsFor("memoryGB", 1).length).toBeGreaterThanOrEqual(2);
  });
});
