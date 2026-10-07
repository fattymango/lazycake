import { describe, expect, it } from "vitest";
import { defaultOffer, maxOffer, offerProblems, offerRequest, parseCapacity } from "./offer";

const caps = { cores: 12, memoryMB: 15314, diskMB: 441802, networkMbps: 1000 };

describe("parseCapacity", () => {
  it("reads the line `agent capacity` prints", () => {
    expect(parseCapacity("cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000")).toEqual(caps);
    expect(parseCapacity("noise before\ncores=4 memory_mb=8000 disk_mb=100000 network_mbps=0\n")).toEqual({
      cores: 4,
      memoryMB: 8000,
      diskMB: 100000,
      networkMbps: null, // the link speed couldn't be read, so nothing to check a network offer against
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

  it("accepts a sensible offer, and exactly what the machine has", () => {
    expect(offerProblems(ok, caps)).toEqual({});
    expect(offerProblems({ cores: 12, memoryGB: 15314 / 1024, storageGB: 441802 / 1024, networkMbps: 1000 }, caps)).toEqual({});
  });

  it("names every value that is bigger than the machine, in plain words", () => {
    const p = offerProblems({ cores: 13, memoryGB: 16, storageGB: 500, networkMbps: 2000 }, caps);
    expect(p.cores).toBe("This machine only has 12 cores.");
    expect(p.memoryGB).toBe("This machine only has 14.95 GB.");
    expect(p.storageGB).toBe("This machine only has 431.44 GB free.");
    expect(p.networkMbps).toBe("This machine's network link is 1000 Mbps.");
  });

  it("without the machine's numbers it only catches nonsense (the agent still refuses oversize offers)", () => {
    expect(offerProblems({ cores: 64, memoryGB: 512, storageGB: 9000, networkMbps: 5000 }, null)).toEqual({});
    expect(offerProblems({ ...ok, cores: 0 }, null).cores).toBeDefined();
    expect(offerProblems({ ...ok, cores: Number.NaN }, null).cores).toBeDefined();
    expect(offerProblems({ ...ok, memoryGB: 0.1 }, null).memoryGB).toMatch(/At least/);
    expect(offerProblems({ ...ok, networkMbps: 1_000_000 }, null).networkMbps).toMatch(/At most/);
  });

  it("can't check the network against a link whose speed is unknown", () => {
    const unknown = { ...caps, networkMbps: null };
    expect(offerProblems({ ...ok, networkMbps: 5000 }, unknown)).toEqual({});
  });
});

describe("defaults and request", () => {
  it("never default above what the machine has", () => {
    expect(defaultOffer(null)).toEqual({ cores: 2, memoryGB: 2, storageGB: 8, networkMbps: 100 });
    const tiny = { cores: 1, memoryMB: 1024, diskMB: 4096, networkMbps: 50 };
    expect(defaultOffer(tiny)).toEqual({ cores: 1, memoryGB: 1, storageGB: 4, networkMbps: 50 });
    expect(offerProblems(defaultOffer(tiny), tiny)).toEqual({});
    expect(maxOffer(tiny).memoryGB).toBe(1);
  });

  it("sends whole MB and Mbps", () => {
    expect(offerRequest({ cores: 6.5, memoryGB: 12, storageGB: 200, networkMbps: 940.4 })).toEqual({
      cores: 6.5,
      memory_mb: 12288,
      disk_mb: 204800,
      network_mbps: 940,
    });
  });
});
