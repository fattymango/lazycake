import { useEffect, useId, useRef, useState } from "react";
import type { KeyboardEvent, MouseEvent } from "react";
import { cn } from "@/lib/cn";
import { niceMax, stackTotal, type StackPoint } from "@/lib/series";

export interface ChartSeries {
  key: string;
  label: string;
  /** Any CSS colour, normally a theme token such as "rgb(var(--accent))". */
  color: string;
  dashed?: boolean;
}

const PAD = { left: 52, right: 10, top: 12, bottom: 6 };

/**
 * One line chart for every time series in the app: a line per series, a faint fill when there is
 * only one, a dashed limit line, hatched gaps where nothing was reported (the line breaks there),
 * and a crosshair whose tooltip lists every series at that moment. Hover or use the arrow keys.
 * The chart carries a spoken summary, so the numbers are never only in a picture.
 */
export function LineChart({
  points,
  series,
  format,
  formatTime,
  axisLabel,
  summary,
  height = 176,
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
  height?: number;
  emptyLabel?: string;
  /** A ceiling to draw as a dashed line (e.g. the cores on offer); the axis always reaches it. */
  limit?: { value: number; label: string };
  /** What a gap (a point with no data) means, shown when it is hovered. */
  gapLabel?: string;
  /** List every series in the tooltip even when its value is zero (a real reading of zero is information). */
  showZero?: boolean;
  /** Extra tooltip line for a hovered point, e.g. "1.4 of 2 cores". */
  footer?: (p: StackPoint) => string | null;
  className?: string;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(640);
  const [active, setActive] = useState<number | null>(null);
  const hatch = useId();

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const measure = () => setWidth(Math.max(240, Math.round(el.clientWidth)));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const n = points.length;
  const max = Math.max(...points.flatMap((p) => series.map((s) => p.values[s.key] ?? 0)), 0);
  // The axis reaches the limit line unless the limit is so far above the data that including it would
  // flatten every line (e.g. 0.5 cores in use of 8 offered); then it is noted at the top edge instead.
  const dataTop = niceMax(max);
  const limitOnScale = !!limit && limit.value > 0 && limit.value <= dataTop * 4;
  const top = limitOnScale && limit ? niceMax(Math.max(max, limit.value)) : dataTop;
  const plotW = width - PAD.left - PAD.right;
  const plotH = height - PAD.top - PAD.bottom;
  const x = (i: number) => PAD.left + (n <= 1 ? plotW / 2 : (i / (n - 1)) * plotW);
  const y = (v: number) => PAD.top + plotH - (Math.min(v, top) / top) * plotH;
  const hasGaps = points.some((p) => p.gap);
  const fill = series.length === 1;

  // Contiguous runs of real data, so a gap breaks the line instead of being drawn through.
  const runs: number[][] = [];
  let cur: number[] = [];
  points.forEach((p, i) => {
    if (p.gap) {
      if (cur.length) runs.push(cur);
      cur = [];
    } else cur.push(i);
  });
  if (cur.length) runs.push(cur);

  const gapSpans: [number, number][] = [];
  let start = -1;
  points.forEach((p, i) => {
    if (p.gap && start < 0) start = i;
    if ((!p.gap || i === n - 1) && start >= 0) {
      gapSpans.push([start, p.gap ? i : i - 1]);
      start = -1;
    }
  });
  const half = n <= 1 ? plotW / 2 : plotW / (n - 1) / 2;

  const labelEvery = Math.max(1, Math.ceil(n / 6));
  const activePoint = active !== null ? points[active] : null;
  const rightHalf = active !== null && active > n / 2;

  const pick = (e: MouseEvent<SVGRectElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const f = (e.clientX - rect.left - (PAD.left - (e.currentTarget.x.baseVal.value || 0))) / plotW;
    setActive(n <= 1 ? 0 : Math.max(0, Math.min(n - 1, Math.round(f * (n - 1)))));
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "ArrowLeft") setActive((a) => Math.max(0, (a ?? n) - 1));
    else if (e.key === "ArrowRight") setActive((a) => Math.min(n - 1, (a ?? -1) + 1));
    else if (e.key === "Escape") setActive(null);
    else return;
    e.preventDefault();
  };

  const line = (idx: number[], key: string) =>
    idx.map((i, k) => `${k ? "L" : "M"}${x(i).toFixed(1)},${y(points[i].values[key] ?? 0).toFixed(1)}`).join(" ");

  return (
    <div className={cn("min-w-0", className)}>
      <div
        ref={wrap}
        role="img"
        aria-label={summary}
        tabIndex={0}
        onKeyDown={onKey}
        onBlur={() => setActive(null)}
        data-chart
        data-points={n}
        data-gaps={points.filter((p) => p.gap).length}
        className="relative rounded-md outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
      >
        <svg width={width} height={height} className="block" onMouseLeave={() => setActive(null)}>
          <defs>
            <pattern id={hatch} width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
              <line x1="0" y1="0" x2="0" y2="5" stroke="rgb(var(--border))" strokeWidth="1.5" />
            </pattern>
          </defs>

          {[0, 0.5, 1].map((f) => (
            <g key={f}>
              <line
                x1={PAD.left}
                x2={width - PAD.right}
                y1={y(top * f)}
                y2={y(top * f)}
                stroke="rgb(var(--border))"
                strokeOpacity={f === 0 ? 1 : 0.7}
              />
              <text x={PAD.left - 8} y={y(top * f)} textAnchor="end" dominantBaseline="middle" className="fill-subtle text-2xs" data-tnum>
                {format(top * f)}
              </text>
            </g>
          ))}

          {gapSpans.map(([a, b]) => (
            <rect
              key={a}
              x={x(a) - half}
              y={PAD.top}
              width={Math.max(2, x(b) - x(a) + 2 * half)}
              height={plotH}
              fill={`url(#${hatch})`}
              opacity={0.7}
            />
          ))}

          {limit && limit.value > 0 && !limitOnScale && (
            <text x={width - PAD.right} y={PAD.top + 10} textAnchor="end" className="fill-warning text-2xs">
              {`↑ ${limit.label}`}
            </text>
          )}
          {limit && limitOnScale && (
            <g>
              <line
                x1={PAD.left}
                x2={width - PAD.right}
                y1={y(limit.value)}
                y2={y(limit.value)}
                stroke="rgb(var(--warning))"
                strokeOpacity={0.85}
                strokeDasharray="5 4"
              />
              <text x={width - PAD.right} y={y(limit.value) - 5} textAnchor="end" className="fill-warning text-2xs">
                {limit.label}
              </text>
            </g>
          )}

          {series.map((s) =>
            runs.map((idx) => {
              const d = line(idx, s.key);
              return (
                <g key={`${s.key}-${idx[0]}`}>
                  {fill && idx.length > 1 && (
                    <path
                      d={`${d} L${x(idx[idx.length - 1]).toFixed(1)},${y(0)} L${x(idx[0]).toFixed(1)},${y(0)} Z`}
                      fill={s.color}
                      opacity={0.12}
                    />
                  )}
                  {idx.length > 1 ? (
                    <path
                      d={d}
                      fill="none"
                      stroke={s.color}
                      strokeWidth={2}
                      strokeLinejoin="round"
                      strokeLinecap="round"
                      strokeDasharray={s.dashed ? "5 4" : undefined}
                    />
                  ) : (
                    <circle cx={x(idx[0])} cy={y(points[idx[0]].values[s.key] ?? 0)} r={3} fill={s.color} />
                  )}
                </g>
              );
            })
          )}

          {activePoint && active !== null && (
            <g pointerEvents="none">
              <line x1={x(active)} x2={x(active)} y1={PAD.top} y2={PAD.top + plotH} stroke="rgb(var(--fg))" strokeOpacity={0.3} />
              {!activePoint.gap &&
                series.map((s) => (
                  <circle
                    key={s.key}
                    cx={x(active)}
                    cy={y(activePoint.values[s.key] ?? 0)}
                    r={4}
                    fill={s.color}
                    stroke="rgb(var(--surface))"
                    strokeWidth={2}
                  />
                ))}
            </g>
          )}

          <rect x={PAD.left} y={PAD.top} width={plotW} height={plotH} fill="transparent" onMouseMove={pick} onMouseEnter={pick} />
        </svg>

        {max === 0 && !hasGaps && n > 0 && (
          <p className="pointer-events-none absolute inset-x-0 top-1/3 flex justify-center text-xs text-subtle">{emptyLabel}</p>
        )}

        {activePoint && (
          <div
            role="status"
            data-chart-tip
            className={cn(
              "pointer-events-none absolute top-4 z-20 w-56 max-w-[70%] rounded-lg border border-border bg-overlay px-3 py-2 text-xs shadow-pop",
              rightHalf ? "left-14" : "right-3"
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
                      <span className="size-2 shrink-0 rounded-full" style={{ background: s.color }} aria-hidden />
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

      <div className="relative mt-1 h-4" style={{ marginLeft: PAD.left, marginRight: PAD.right }} aria-hidden>
        {points.map((p, i) =>
          i % labelEvery === 0 ? (
            <span
              key={p.at}
              className="absolute -translate-x-1/2 whitespace-nowrap text-2xs text-subtle"
              style={{ left: `${n <= 1 ? 50 : (i / (n - 1)) * 100}%` }}
            >
              {axisLabel ? axisLabel(p.at) : formatTime(p.at).split(",")[0]}
            </span>
          ) : null
        )}
      </div>

      <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1">
        {series.map((s) => (
          <li key={s.key} className="flex items-center gap-1.5 text-xs text-muted">
            <span className="size-2 rounded-full" style={{ background: s.color }} aria-hidden />
            {s.label}
          </li>
        ))}
      </ul>
    </div>
  );
}
