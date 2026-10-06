import { describe, expect, it } from "vitest";
import { dailySpend } from "./spend";
import type { LedgerEntry } from "./types";

const day = 86_400_000;
// The ledger stores charges as negative amounts and credits as positive (store.LedgerEntry).
const entry = (id: string, at: number, micros: number, kind: LedgerEntry["kind"] = "charge"): LedgerEntry => ({
  id,
  task_id: "tsk_" + id,
  kind,
  amount_micros: kind === "charge" ? -micros : micros,
  created_at_ms: at,
});

describe("dailySpend", () => {
  const now = new Date(2026, 9, 6, 15, 0, 0); // local time, mid-afternoon
  const startOfToday = new Date(2026, 9, 6).getTime();

  it("puts charges in the right day and totals them", () => {
    const { bars, total } = dailySpend(
      [entry("a", startOfToday + 1000, 5000), entry("b", startOfToday + 3600_000, 2500), entry("c", startOfToday - day + 1000, 1000)],
      7,
      now
    );
    expect(bars).toHaveLength(7);
    expect(bars[6].value).toBe(7500); // today
    expect(bars[5].value).toBe(1000); // yesterday
    expect(total).toBe(8500);
  });
  it("ignores credits and anything older than the window", () => {
    const { total } = dailySpend([entry("a", startOfToday, 5000, "credit"), entry("b", startOfToday - 10 * day, 9000)], 7, now);
    expect(total).toBe(0);
  });
  it("reports spend as a positive amount even though charges are stored negative", () => {
    const { bars, total } = dailySpend([entry("a", startOfToday + 1000, 4200)], 7, now);
    expect(total).toBe(4200);
    expect(bars[6].value).toBe(4200);
    expect(bars[6].detail).toContain("$0.0042");
  });
  it("copes with no data", () => {
    expect(dailySpend(undefined, 7, now).total).toBe(0);
  });
});
