import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { toneClasses, type Tone } from "./tone";

// Literal class names (not derived from toneClasses) so Tailwind sees them.
const softRing: Record<Tone, string> = {
  neutral: "ring-muted/15",
  accent: "ring-accent/15",
  success: "ring-success/15",
  warning: "ring-warning/15",
  danger: "ring-danger/15",
  info: "ring-info/15",
};

export interface TimelineStep {
  id: string;
  title: ReactNode;
  /** Right-aligned secondary text, usually a timestamp. */
  time?: ReactNode;
  detail?: ReactNode;
  tone?: Tone;
  /** done = filled dot, active = pulsing, pending = hollow. */
  state: "done" | "active" | "pending";
}

/** Vertical sequence of steps joined by a line (a task's lifecycle). */
export function Timeline({ steps }: { steps: TimelineStep[] }) {
  return (
    <ol className="relative">
      {steps.map((s, i) => {
        const t = toneClasses[s.tone ?? "accent"];
        const last = i === steps.length - 1;
        return (
          <li key={s.id} className="relative flex gap-3 pb-5 last:pb-0">
            {!last && (
              <span
                className={cn(
                  "absolute left-[0.4375rem] top-4 h-[calc(100%-0.5rem)] w-px",
                  s.state === "pending" ? "bg-border" : "bg-border-strong"
                )}
                aria-hidden
              />
            )}
            <span className="relative mt-1 flex size-3.5 shrink-0 items-center justify-center" aria-hidden>
              {s.state === "active" && <span className={cn("absolute size-full animate-ping-soft rounded-full opacity-60", t.dot)} />}
              <span
                className={cn(
                  "relative size-2.5 rounded-full border-2",
                  s.state === "pending"
                    ? "border-border-strong bg-surface"
                    : cn(t.dot, "border-transparent ring-4", softRing[s.tone ?? "accent"])
                )}
              />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-3">
                <p className={cn("text-sm font-medium", s.state === "pending" ? "text-muted" : "text-fg")}>{s.title}</p>
                {s.time && (
                  <p className="shrink-0 text-xs text-muted" data-tnum>
                    {s.time}
                  </p>
                )}
              </div>
              {s.detail && <p className="mt-0.5 break-words text-xs leading-5 text-muted">{s.detail}</p>}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
