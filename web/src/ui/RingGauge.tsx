import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { toneClasses, type Tone } from "./tone";

/**
 * A ring that fills to `value` (0..1) with something short in the middle: a percentage at a glance. `summary` is the spoken
 * form, so the number is never only in the picture.
 */
export function RingGauge({
  value,
  tone = "accent",
  size = 112,
  center,
  summary,
  className,
}: {
  /** 0..1; null draws an empty ring. */
  value: number | null;
  tone?: Tone;
  size?: number;
  center?: ReactNode;
  summary: string;
  className?: string;
}) {
  const r = 42;
  const c = 2 * Math.PI * r;
  const filled = value === null ? 0 : Math.max(0, Math.min(1, value));
  return (
    <div className={cn("relative shrink-0", className)} style={{ width: size, height: size }} role="img" aria-label={summary}>
      <svg viewBox="0 0 100 100" className="size-full -rotate-90" aria-hidden>
        <circle cx="50" cy="50" r={r} fill="none" strokeWidth="9" className="stroke-border" />
        <circle
          cx="50"
          cy="50"
          r={r}
          fill="none"
          strokeWidth="9"
          strokeLinecap="round"
          strokeDasharray={`${c * filled} ${c}`}
          className={cn("stroke-current transition-[stroke-dasharray] duration-500", toneClasses[tone].text)}
        />
      </svg>
      {center && <div className="absolute inset-0 flex items-center justify-center text-center">{center}</div>}
    </div>
  );
}
