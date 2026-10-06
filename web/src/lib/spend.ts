import type { LedgerEntry } from "./types";
import type { Bar } from "@/ui/BarChart";
import { money } from "./format";

/** Ledger totals of one kind per local calendar day for the last `days` days, oldest first. */
export function dailyTotals(
  entries: LedgerEntry[] | undefined,
  days: number,
  kind: LedgerEntry["kind"],
  now = new Date()
): { bars: Bar[]; total: number } {
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (days - 1)).getTime();
  const buckets = Array.from({ length: days }, () => 0);
  for (const e of entries ?? []) {
    if (e.kind !== kind || e.created_at_ms < start) continue;
    const idx = Math.floor((e.created_at_ms - start) / 86_400_000);
    if (idx >= 0 && idx < days) buckets[idx] += e.amount_micros;
  }
  const bars = buckets.map((value, i) => {
    const d = new Date(start + i * 86_400_000);
    return {
      label: days <= 8 ? d.toLocaleDateString(undefined, { weekday: "short" }) : i % 3 === 0 ? String(d.getDate()) : "",
      value,
      detail: `${d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" })}: ${money(value)}`,
    };
  });
  return { bars, total: buckets.reduce((a, b) => a + b, 0) };
}

/** What a customer was charged per day. */
export const dailySpend = (entries: LedgerEntry[] | undefined, days: number, now = new Date()) => dailyTotals(entries, days, "charge", now);

/** What a provider earned per day. */
export const dailyEarnings = (entries: LedgerEntry[] | undefined, days: number, now = new Date()) =>
  dailyTotals(entries, days, "credit", now);
