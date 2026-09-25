export function money(micros: number | undefined): string {
  return "$" + ((micros ?? 0) / 1e6).toFixed(4);
}

export function dateTime(ms: number | undefined): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString();
}

export function relativeTime(ms: number | undefined): string {
  if (!ms) return "—";
  const deltaS = (Date.now() - ms) / 1000;
  if (deltaS < 60) return `${Math.max(0, Math.round(deltaS))}s ago`;
  if (deltaS < 3600) return `${Math.round(deltaS / 60)}m ago`;
  if (deltaS < 86400) return `${Math.round(deltaS / 3600)}h ago`;
  return `${Math.round(deltaS / 86400)}d ago`;
}
