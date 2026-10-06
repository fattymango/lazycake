import { describe, expect, it } from "vitest";
import { shortenId } from "./Identifier";

describe("shortenId", () => {
  it("keeps the type prefix and the distinguishing tail", () => {
    expect(shortenId("tsk_01M4170DCF1506FFA2A58939F6")).toBe("tsk_01M4…8939F6");
    expect(shortenId("nod_01M4C82561EC215A6E31807CEE")).toBe("nod_01M4…807CEE");
  });
  it("leaves short values alone", () => {
    expect(shortenId("tsk_abc")).toBe("tsk_abc");
    expect(shortenId("localhost")).toBe("localhost");
  });
  it("shortens ids with no recognised prefix", () => {
    expect(shortenId("A".repeat(60))).toBe("AAAA…AAAAAA");
  });
  it("two ids that differ only at the end stay distinguishable", () => {
    expect(shortenId("tsk_01M4170DCF1506FFA2A58939F6")).not.toBe(shortenId("tsk_01M4170DCF1506FFA2A58939F7"));
  });
});
