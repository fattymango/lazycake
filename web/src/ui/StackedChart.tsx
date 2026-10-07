import { useId, useState } from "react";
import { cn } from "@/lib/cn";
import { niceMax, stackTotal, type StackPoint } from "@/lib/series";

export interface ChartSeries {
  key: string;
  label: string;
  /** Tailwind background class for the segment and its legend dot, e.g. "bg-accent". */
  className: string;
}

/**
 * A stacked column chart over time. Hovering (or focusing) a column shows what
 * every series contributed to it, so the same chart answers both "how much" and
 * "who". Pure CSS columns: no chart library, scales with its container, and the
 * whole chart carries a spoken summary so the numbers are never only in a picture.
 */
export function StackedChart({
  points,
  series,
  format,
  formatTime,
  axisLabel,
  summary,
  heightClass = "h-40",
  emptyLabel = "Nothing in this period",
  limit,
  gapLabel = "No data: the machine was offline",
  showZero = false,
  footer,
  className,
}: {
  points: StackPoint[];
  series: ChartSeries[];
  format: (value: number) => string;
  formatTime: (at: number) => string;
  /** The short label under the axis (default: the date part of formatTime). */
  axisLabel?: (at: number) => string;
  summary: string;
  heightClass?: string;
  emptyLabel?: string;
  /** A ceiling to draw as a dashed line (e.g. the cores on offer); the axis always reaches it. */
  limit?: { value: number; label: string };
  /** List every series in the tooltip even when its value is zero (a real reading of zero is information). */
  showZero?: boolean;
  /** What a gap (a point with no data) means, shown when it is hovered. */
  gapLabel?: string;
  /** Extra tooltip line for a hovered column, e.g. "1.4 of 2 cores". */
  footer?: (p: StackPoint) => string | null;
  className?: string;
}) {
  const [active, setActive] = useState<number | null>(null);
  const id = useId();
  const max = Math.max(...points.map(stackTotal), 0);
  const top = niceMax(Math.max(max, limit?.value ?? 0));
  const n = points.length;

  const labelEvery = Math.max(1, Math.ceil(n / 6));
  const activePoint = active !== null ? points[active] : null;
  // Keep the tooltip inside the chart: anchor it to whichever side has room.
  const rightHalf = active !== null && active > n / 2;

  return (
    <div className={cn("min-w-0", className)}>
      <div role="img" aria-label={summary} className="relative">
        <div className={cn("relative flex items-end gap-px", heightClass)} onMouseLeave={() => setActive(null)}>
          {[0, 0.5, 1].map((f) => (
            <div
              key={f}
              aria-hidden
              className="pointer-events-none absolute inset-x-0 border-t border-border/70"
              style={{ bottom: `${f * 100}%` }}
            >
              <span className="absolute -top-2.5 left-0 rounded bg-surface px-1 text-2xs text-subtle" data-tnum>
                {format(top * f)}
              </span>
            </div>
          ))}
          {limit && limit.value > 0 && (
            <div
              aria-hidden
              className="pointer-events-none absolute inset-x-0 z-10 border-t border-dashed border-warning/80"
              style={{ bottom: `${(limit.value / top) * 100}%` }}
            >
              <span className="absolute -top-4 right-0 rounded bg-surface px-1 text-2xs text-warning">{limit.label}</span>
            </div>
          )}
          {points.map((p, i) => {
            const total = stackTotal(p);
            return (
              <div
                key={p.at}
                tabIndex={0}
                aria-label={`${formatTime(p.at)}: ${p.gap ? gapLabel : format(total)}`}
                onMouseEnter={() => setActive(i)}
                onFocus={() => setActive(i)}
                onBlur={() => setActive(null)}
                className={cn(
                  "relative flex h-full min-w-0 flex-1 flex-col-reverse rounded-sm outline-none",
                  active === i && "bg-fg/5",
                  p.gap && "bg-[repeating-linear-gradient(135deg,transparent_0_3px,rgb(var(--border)/0.55)_3px_4px)]"
                )}
              >
                {series.map((s) => {
                  const v = p.values[s.key] ?? 0;
                  if (v <= 0) return null;
                  return (
                    <div
                      key={s.key}
                      className={cn(
                        "w-full first:rounded-b-[1px] last:rounded-t-[2px]",
                        s.className,
                        active === i ? "opacity-100" : "opacity-80"
                      )}
                      style={{ height: `${Math.max((v / top) * 100, 1.5)}%` }}
                    />
                  );
                })}
              </div>
            );
          })}
        </div>

        {max === 0 && !points.some((p) => p.gap) && (
          <p className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-subtle">{emptyLabel}</p>
        )}

        {activePoint && (
          <div
            id={`${id}-tip`}
            role="status"
            className={cn(
              "pointer-events-none absolute top-3 z-20 w-56 max-w-[70%] rounded-lg border border-border bg-overlay px-3 py-2 text-xs shadow-pop",
              rightHalf ? "left-2" : "right-2"
            )}
          >
            <p className="mb-1 font-medium text-fg">{formatTime(activePoint.at)}</p>
            <ul className="space-y-0.5">
              {series
                .filter((s) => showZero || (activePoint.values[s.key] ?? 0) > 0)
                .sort((a, b) => (activePoint.values[b.key] ?? 0) - (activePoint.values[a.key] ?? 0))
                .map((s) => (
                  <li key={s.key} className="flex items-center justify-between gap-3">
                    <span className="flex min-w-0 items-center gap-1.5 text-muted">
                      <span className={cn("size-2 shrink-0 rounded-sm", s.className)} aria-hidden />
                      <span className="truncate">{s.label}</span>
                    </span>
                    <span className="shrink-0 text-fg" data-tnum>
                      {format(activePoint.values[s.key] ?? 0)}
                    </span>
                  </li>
                ))}
              {activePoint.gap && <li className="text-subtle">{gapLabel}</li>}
              {!activePoint.gap && !showZero && stackTotal(activePoint) === 0 && <li className="text-subtle">Nothing</li>}
            </ul>
            {footer && !activePoint.gap && footer(activePoint) && (
              <p className="mt-1.5 border-t border-border pt-1.5 text-muted" data-tnum>
                {footer(activePoint)}
              </p>
            )}
          </div>
        )}
      </div>

      <div className="mt-1.5 flex gap-px" aria-hidden>
        {points.map((p, i) => (
          <span key={p.at} className="min-w-0 flex-1 overflow-visible whitespace-nowrap text-2xs text-subtle">
            {i % labelEvery === 0 ? (axisLabel ? axisLabel(p.at) : formatTime(p.at).split(",")[0]) : ""}
          </span>
        ))}
      </div>

      <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1">
        {series.map((s) => (
          <li key={s.key} className="flex items-center gap-1.5 text-xs text-muted">
            <span className={cn("size-2 rounded-sm", s.className)} aria-hidden />
            {s.label}
          </li>
        ))}
      </ul>
    </div>
  );
}
