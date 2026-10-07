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
  summary,
  heightClass = "h-40",
  emptyLabel = "Nothing in this period",
  className,
}: {
  points: StackPoint[];
  series: ChartSeries[];
  format: (value: number) => string;
  formatTime: (at: number) => string;
  summary: string;
  heightClass?: string;
  emptyLabel?: string;
  className?: string;
}) {
  const [active, setActive] = useState<number | null>(null);
  const id = useId();
  const max = Math.max(...points.map(stackTotal), 0);
  const top = niceMax(max);
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
          {points.map((p, i) => {
            const total = stackTotal(p);
            return (
              <div
                key={p.at}
                tabIndex={0}
                aria-label={`${formatTime(p.at)}: ${format(total)}`}
                onMouseEnter={() => setActive(i)}
                onFocus={() => setActive(i)}
                onBlur={() => setActive(null)}
                className={cn("relative flex h-full min-w-0 flex-1 flex-col-reverse rounded-sm outline-none", active === i && "bg-fg/5")}
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

        {max === 0 && (
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
                .filter((s) => (activePoint.values[s.key] ?? 0) > 0)
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
              {stackTotal(activePoint) === 0 && <li className="text-subtle">Nothing</li>}
            </ul>
          </div>
        )}
      </div>

      <div className="mt-1.5 flex gap-px" aria-hidden>
        {points.map((p, i) => (
          <span key={p.at} className="min-w-0 flex-1 overflow-visible whitespace-nowrap text-2xs text-subtle">
            {i % labelEvery === 0 ? formatTime(p.at).split(",")[0] : ""}
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
