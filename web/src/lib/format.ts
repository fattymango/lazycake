// Display formatting. Every number a person reads goes through here so units
// and precision stay consistent across pages.

const usd2 = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });
const usd4 = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 4, maximumFractionDigits: 4 });
const integer = new Intl.NumberFormat("en-US");

/** Micro-dollars -> "$24.32"; sub-dollar amounts keep 4 places so tiny charges are visible. */
export function money(micros: number | undefined | null): string {
  const dollars = (micros ?? 0) / 1e6;
  return Math.abs(dollars) >= 1 || dollars === 0 ? usd2.format(dollars) : usd4.format(dollars);
}

/** Signed variant for ledgers: "+$0.0042" / "-$0.0042". */
export function signedMoney(micros: number, sign: 1 | -1): string {
  return (sign > 0 ? "+" : "−") + money(Math.abs(micros));
}

export function count(n: number | undefined | null): string {
  return integer.format(n ?? 0);
}

export function cpu(cores: number | undefined): string {
  if (cores === undefined) return "—";
  return `${Number(cores.toFixed(2))} vCPU`;
}

/** MB -> "512 MB" / "2 GB" / "1.5 GB". */
export function memory(mb: number | undefined): string {
  if (mb === undefined) return "—";
  if (mb < 1024) return `${mb} MB`;
  const gb = mb / 1024;
  return `${Number(gb.toFixed(gb < 10 ? 1 : 0))} GB`;
}

/** Milliseconds -> "850ms" / "14s" / "2m 05s" / "3h 12m". */
export function duration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null || ms < 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ${String(m % 60).padStart(2, "0")}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function dateTime(ms: number | undefined | null): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function shortDateTime(ms: number | undefined | null): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function clockTime(ms: number): string {
  const d = new Date(ms);
  const p = (n: number, l = 2) => String(n).padStart(l, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}

/** "just now" / "5m ago" / "3h ago" / "2d ago". */
export function relativeTime(ms: number | undefined | null, now = Date.now()): string {
  if (!ms) return "—";
  const s = Math.round((now - ms) / 1000);
  if (s < 5) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  if (d < 30) return `${d}d ago`;
  return new Date(ms).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

/** "docker.io/library/alpine@sha256:c64c…" -> { name: "alpine", registry: "docker.io/library", digest: "c64c687cbea9" }. */
export function parseImage(image: string): { name: string; registry: string; digest: string } {
  const [ref, digest = ""] = image.split("@");
  const parts = ref.split("/");
  return {
    name: parts[parts.length - 1] || ref,
    registry: parts.slice(0, -1).join("/"),
    digest: digest.replace("sha256:", "").slice(0, 12),
  };
}
