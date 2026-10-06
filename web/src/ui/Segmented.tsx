import { cn } from "@/lib/cn";

export interface SegmentOption<V extends string> {
  value: V;
  label: string;
  count?: number;
}

/** A single-choice filter shown as a row of buttons (radio semantics). */
export function Segmented<V extends string>({
  options,
  value,
  onChange,
  label,
  className,
}: {
  options: SegmentOption<V>[];
  value: V;
  onChange: (v: V) => void;
  label: string;
  className?: string;
}) {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={cn("inline-flex max-w-full gap-0.5 overflow-x-auto rounded-lg border border-border bg-raised/60 p-0.5", className)}
      data-scroll-x
    >
      {options.map((o) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(o.value)}
            className={cn(
              "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors",
              active ? "bg-overlay text-fg shadow-sm ring-1 ring-border-strong" : "text-muted hover:text-fg"
            )}
          >
            {o.label}
            {o.count !== undefined && (
              <span className={cn("rounded px-1 text-2xs", active ? "bg-raised text-muted" : "text-subtle")} data-tnum>
                {o.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
