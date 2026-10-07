// Turning a task's own readings into chart columns (task page, "Resource use").
import type { StackPoint } from "./series";
import type { TaskReading } from "./types";

/** Readings arrive about every 15 s; a column narrower than 20 s could land between two of them. */
export const MIN_COLUMN_MS = 20_000;
export const TARGET_COLUMNS = 60;

export type TaskMetric = "cpu" | "memory" | "network";

export interface Column {
  at: number;
  /** How many readings fell in this column; 0 means none arrived (a gap). */
  n: number;
  cpu: number; // average cores
  memory: number; // average bytes
  out: number; // bytes out of the container during the column (a sum)
  in: number; // bytes into the container
}

/**
 * Group readings into evenly spaced columns from `startMs` to `endMs`: CPU and memory are
 * averaged, network bytes are summed. A column with no reading is kept (n = 0) so a stretch where
 * nothing was reported shows as a gap rather than being squeezed out.
 */
export function bucketReadings(readings: TaskReading[], startMs: number, endMs: number): Column[] {
  if (readings.length === 0) return [];
  const start = Math.min(startMs, readings[0].at_ms);
  const end = Math.max(endMs, readings[readings.length - 1].at_ms);
  const width = Math.max(MIN_COLUMN_MS, Math.ceil((end - start) / TARGET_COLUMNS));
  const count = Math.floor((end - start) / width) + 1;
  const cols: Column[] = Array.from({ length: count }, (_, i) => ({ at: start + i * width, n: 0, cpu: 0, memory: 0, out: 0, in: 0 }));
  for (const r of readings) {
    const c = cols[Math.min(count - 1, Math.floor((r.at_ms - start) / width))];
    c.n++;
    c.cpu += r.cpu_cores;
    c.memory += r.memory_bytes;
    c.out += r.tunnel_out_bytes;
    c.in += r.tunnel_in_bytes;
  }
  for (const c of cols) {
    if (c.n > 0) {
      c.cpu /= c.n;
      c.memory /= c.n;
    }
  }
  return cols;
}

export function taskStack(cols: Column[], metric: TaskMetric): StackPoint[] {
  return cols.map((c): StackPoint => {
    if (c.n === 0) return { at: c.at, values: {}, gap: true };
    if (metric === "cpu") return { at: c.at, values: { cpu: c.cpu } };
    if (metric === "memory") return { at: c.at, values: { memory: c.memory } };
    return { at: c.at, values: { out: c.out, in: c.in } };
  });
}

export const MIN_TRAFFIC_COLUMN_MS = 10_000;

export interface GatewayTrafficPoint {
  at_ms: number;
  /** Empty for traffic recorded before gateways were tracked per sample. */
  gateway_id: string;
  received: number;
  sent: number;
}

export interface GatewayBuckets {
  points: StackPoint[];
  /** Gateway keys, the one that moved the most first. */
  gateways: string[];
  /** What each gateway moved over the whole task. */
  totals: Record<string, { received: number; sent: number }>;
}

/** The series key for a gateway ("" -> a fixed key, since an empty key can't label a line). */
export const gatewayKey = (id: string) => id || "unknown";

/**
 * Gateway traffic per column from `startMs` to `endMs`, one value per gateway (bytes both ways added up),
 * so a hover can say how much each gateway moved. The gateway only reports while bytes move, so a column
 * with nothing in it is a real zero (the line drops to the axis), never a gap. `received` and `sent`
 * (all gateways together) ride along on every point for the tooltip footer.
 */
export function bucketTraffic(series: GatewayTrafficPoint[], startMs: number, endMs: number): GatewayBuckets {
  const empty: GatewayBuckets = { points: [], gateways: [], totals: {} };
  if (series.length === 0) return empty;
  const start = Math.min(startMs, series[0].at_ms);
  const end = Math.max(endMs, series[series.length - 1].at_ms);
  const width = Math.max(MIN_TRAFFIC_COLUMN_MS, Math.ceil((end - start) / TARGET_COLUMNS));
  const count = Math.floor((end - start) / width) + 1;
  const totals: GatewayBuckets["totals"] = {};
  for (const p of series) {
    const k = gatewayKey(p.gateway_id);
    totals[k] ??= { received: 0, sent: 0 };
    totals[k].received += p.received;
    totals[k].sent += p.sent;
  }
  const gateways = Object.keys(totals).sort(
    (a, b) => totals[b].received + totals[b].sent - (totals[a].received + totals[a].sent) || a.localeCompare(b)
  );
  const points: StackPoint[] = Array.from({ length: count }, (_, i) => ({
    at: start + i * width,
    values: { ...Object.fromEntries(gateways.map((g) => [g, 0])), received: 0, sent: 0 },
  }));
  for (const p of series) {
    const c = points[Math.min(count - 1, Math.floor((p.at_ms - start) / width))];
    c.values[gatewayKey(p.gateway_id)] += p.received + p.sent;
    c.values.received += p.received;
    c.values.sent += p.sent;
  }
  return { points, gateways, totals };
}
