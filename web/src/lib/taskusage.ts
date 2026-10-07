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
