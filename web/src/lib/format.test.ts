import { describe, expect, it } from "vitest";
import { bytes, cores, cpu, duration, memory, money, parseImage, relativeTime, signedMoney } from "./format";

describe("money", () => {
  it("uses 2 places for dollars and 4 for sub-dollar amounts", () => {
    expect(money(24_317_500)).toBe("$24.32");
    expect(money(4_200)).toBe("$0.0042");
    expect(money(0)).toBe("$0.00");
    expect(money(undefined)).toBe("$0.00");
  });
  it("signs ledger amounts", () => {
    expect(signedMoney(4_200, -1)).toBe("−$0.0042");
    expect(signedMoney(2_500_000, 1)).toBe("+$2.50");
  });
});

describe("units", () => {
  it("formats cores and memory", () => {
    expect(cpu(0.5)).toBe("0.5 vCPU");
    expect(cpu(2)).toBe("2 vCPU");
    expect(memory(512)).toBe("512 MB");
    expect(memory(1024)).toBe("1 GB");
    expect(memory(1536)).toBe("1.5 GB");
    expect(memory(131072)).toBe("128 GB");
  });
  it("formats durations", () => {
    expect(duration(850)).toBe("850ms");
    expect(duration(14_000)).toBe("14s");
    expect(duration(125_000)).toBe("2m 05s");
    expect(duration(3 * 3600_000 + 12 * 60_000)).toBe("3h 12m");
    expect(duration(undefined)).toBe("—");
  });
});

describe("relativeTime", () => {
  const now = 1_000_000_000_000;
  it("reads naturally at each scale", () => {
    expect(relativeTime(now - 2_000, now)).toBe("just now");
    expect(relativeTime(now - 30_000, now)).toBe("30s ago");
    expect(relativeTime(now - 5 * 60_000, now)).toBe("5m ago");
    expect(relativeTime(now - 3 * 3600_000, now)).toBe("3h ago");
    expect(relativeTime(now - 2 * 86400_000, now)).toBe("2d ago");
    expect(relativeTime(undefined, now)).toBe("—");
  });
});

describe("parseImage", () => {
  it("splits a digest-pinned reference into name, registry and short digest", () => {
    const img = "docker.io/fattymango/lcbench@sha256:65e8c5765cc43af8cf9e6a71165d31954dbfee495aa1ada997fb35b8c672e1db";
    expect(parseImage(img)).toEqual({ name: "lcbench", registry: "docker.io/fattymango", digest: "65e8c5765cc4" });
  });
  it("copes with a reference that has no digest", () => {
    expect(parseImage("alpine")).toEqual({ name: "alpine", registry: "", digest: "" });
  });
});

describe("bytes", () => {
  it("scales through decimal units without ever printing 1000 of something", () => {
    expect(bytes(0)).toBe("0 B");
    expect(bytes(undefined)).toBe("0 B");
    expect(bytes(812)).toBe("812 B");
    expect(bytes(4_500)).toBe("4.5 kB");
    expect(bytes(999_999)).toBe("1 MB");
    expect(bytes(1_230_000_000)).toBe("1.23 GB");
    expect(bytes(52_400_000)).toBe("52.4 MB");
    expect(bytes(740_000_000_000)).toBe("740 GB");
  });
});

describe("cores", () => {
  it("never shows a tiny real amount as zero", () => {
    expect(cores(0)).toBe("0");
    expect(cores(undefined)).toBe("0");
    expect(cores(0.0017)).toBe("<0.01");
    expect(cores(0.5)).toBe("0.5");
    expect(cores(1.2549)).toBe("1.25");
  });
});
