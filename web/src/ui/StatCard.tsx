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
  className,
}: {
  label: string;
  value: ReactNode;
  /** Small line under the value (a unit, a comparison, a hint). */
  sub?: ReactNode;
  icon?: LucideIcon;
  tone?: Tone;
  trend?: number[];
  loading?: boolean;
  action?: ReactNode;
  className?: string;
}) {
  const t = toneClasses[tone];
  return (
    <Card className={cn("relative flex flex-col gap-3 overflow-hidden p-5", className)}>
      <div className="flex items-center justify-between gap-3">
        <span className="min-w-0 truncate text-xs font-medium text-muted">{label}</span>
        {Icon && (
          <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-lg border", t.bg, t.border, t.text)}>
            <Icon className="size-4" aria-hidden />
          </span>
        )}
      </div>
      <div className="min-w-0">
        {loading ? (
          <Skeleton className="h-8 w-28" />
        ) : (
          <div className="truncate text-[1.75rem] font-semibold leading-9 tracking-tight text-fg" data-tnum>
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
