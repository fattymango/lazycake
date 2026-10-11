import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/cn";
import { Card } from "./Card";
import { Skeleton } from "./Skeleton";
import { Sparkline } from "./Sparkline";
import { toneClasses, type Tone } from "./tone";

export function StatCard({
  label,
  value,
  sub,
  icon: Icon,
  tone = "neutral",
  trend,
  loading,
  action,
  compact,
  className,
}: {
  label: ReactNode;
  value: ReactNode;
  /** Small line under the value (a unit, a comparison, a hint). */
  sub?: ReactNode;
  icon?: LucideIcon;
  tone?: Tone;
  trend?: number[];
  loading?: boolean;
  action?: ReactNode;
  /** A smaller tile for a side rail. */
  compact?: boolean;
  className?: string;
}) {
  const t = toneClasses[tone];
  return (
    <Card className={cn("relative flex flex-col overflow-hidden", compact ? "gap-2 p-4" : "gap-3 p-5", className)}>
      <div className="flex items-center justify-between gap-3">
        <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium text-muted">{label}</span>
        {Icon && (
          <span
            className={cn(
              "flex shrink-0 items-center justify-center rounded-lg border",
              compact ? "size-7" : "size-8",
              t.bg,
              t.border,
              t.text
            )}
          >
            <Icon className="size-4" aria-hidden />
          </span>
        )}
      </div>
      <div className="min-w-0">
        {loading ? (
          <Skeleton className="h-8 w-28" />
        ) : (
          <div
            className={cn("truncate font-semibold tracking-tight text-fg", compact ? "text-xl leading-8" : "text-[1.75rem] leading-9")}
            data-tnum
          >
            {value}
          </div>
        )}
        {sub && !loading && <div className="mt-0.5 truncate text-xs text-muted">{sub}</div>}
      </div>
      {trend && !loading && <Sparkline data={trend} tone={tone === "neutral" ? "accent" : tone} className="-mb-1" />}
      {action && <div className="mt-auto pt-1">{action}</div>}
    </Card>
  );
}
