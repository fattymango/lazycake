import { cn } from "@/lib/cn";
import { Tooltip } from "./Tooltip";
import { toneClasses, type Tone } from "./tone";

export interface Bar {
  label: string;
  value: number;
  /** Shown in the tooltip, e.g. "Mon 6 Oct: $0.42". */
  detail: string;
}

/**
 * Small column chart. Every bar has a tooltip and the whole chart carries an
 * accessible summary, so it is never the only carrier of the numbers.
 */
export function BarChart({
  data,
  tone = "accent",
  heightClass = "h-32",
  summary,
  className,
}: {
  data: Bar[];
  tone?: Tone;
  heightClass?: string;
  /** Spoken description of the chart. */
  summary: string;
  className?: string;
}) {
  const max = Math.max(...data.map((d) => d.value), 0);
  return (
    <div role="img" aria-label={summary} className={cn("min-w-0", className)}>
      <div className={cn("flex items-end gap-1.5", heightClass)}>
        {data.map((d, i) => {
          const pct = max > 0 ? (d.value / max) * 100 : 0;
          return (
            <Tooltip key={i} content={d.detail}>
              <div className="group/bar flex h-full min-w-0 flex-1 items-end rounded-sm">
                <div
                  className={cn(
                    "w-full rounded-t-[3px] transition-[height,opacity] duration-500",
                    d.value > 0 ? cn(toneClasses[tone].solid, "opacity-80 group-hover/bar:opacity-100") : "bg-border"
                  )}
                  style={{ height: d.value > 0 ? `${Math.max(pct, 4)}%` : "2px" }}
                />
              </div>
            </Tooltip>
          );
        })}
      </div>
      <div className="mt-2 flex gap-1.5" aria-hidden>
        {data.map((d, i) => (
          <span key={i} className="min-w-0 flex-1 truncate text-center text-2xs text-subtle">
            {d.label}
          </span>
        ))}
      </div>
    </div>
  );
}
