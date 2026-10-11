/** A rate in bits per second, written the way network speeds are: "850 kbps", "2.67 Mbps", "1.2 Gbps". Three significant digits, no rounding that hides a small real value. */
export function bitrate(bps: number | null | undefined): string {
  if (bps === null || bps === undefined || !Number.isFinite(bps) || bps < 0) return "—";
  const units: [number, string][] = [
    [1e9, "Gbps"],
    [1e6, "Mbps"],
    [1e3, "kbps"],
  ];
  for (const [size, name] of units) {
    if (bps >= size) {
      // 999.6 kbps must not print as "1000 kbps": round first, then pick the unit again if it rolled over
      const v = Number((bps / size).toPrecision(3));
      if (v >= 1000 && size < 1e9) return bitrate(v * size);
      return `${v} ${name}`;
    }
  }
  return `${Math.round(bps)} bps`;
}
