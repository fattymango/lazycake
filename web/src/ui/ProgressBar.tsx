import { cn } from "@/lib/cn";
import { toneClasses, type Tone } from "./tone";

export function ProgressBar({
  value,
  tone = "accent",
  className,
  label,
}: {
  /** 0..1 */
  value: number;
  tone?: Tone;
  className?: string;
  label: string;
}) {
  const pct = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0)) * 100;
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      className={cn("h-1.5 w-full overflow-hidden rounded-full bg-raised", className)}
    >
      <div className={cn("h-full rounded-full transition-[width] duration-500", toneClasses[tone].solid)} style={{ width: `${pct}%` }} />
    </div>
  );
}
