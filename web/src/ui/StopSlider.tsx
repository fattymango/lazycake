import { cn } from "@/lib/cn";
import { sliderPosition, snapToStop } from "@/lib/offer";

const THUMB_PX = 16;

/**
 * A slider that moves between checkpoints instead of through every number: dragging snaps to the nearest
 * one, and clicking a checkpoint's label jumps to it. A value that isn't a checkpoint (typed in the field next
 * to it) puts the thumb between its neighbours, and a value past the last checkpoint leaves it at the end.
 */
export function StopSlider({
  stops,
  value,
  onChange,
  label,
  className,
}: {
  stops: number[];
  value: number;
  onChange: (v: number) => void;
  label: string;
  className?: string;
}) {
  const last = stops.length - 1;
  return (
    <div className={cn("min-w-0 px-3", className)}>
      <input
        type="range"
        aria-label={label}
        min={0}
        max={last}
        step="any"
        value={sliderPosition(stops, value)}
        onChange={(e) => onChange(snapToStop(stops, Number(e.target.value)))}
        aria-valuetext={String(value)}
        className="block h-5 w-full cursor-pointer accent-[rgb(var(--accent))]"
      />
      <div className="relative mt-0.5 h-5" aria-hidden={false}>
        {stops.map((s, i) => {
          const frac = last === 0 ? 0 : i / last;
          return (
            <button
              key={s}
              type="button"
              tabIndex={-1}
              onClick={() => onChange(s)}
              className={cn(
                "absolute top-0 -translate-x-1/2 rounded px-1 text-2xs tabular-nums transition-colors hover:text-fg",
                value === s ? "font-semibold text-accent" : "text-subtle"
              )}
              style={{ left: `calc(${frac * 100}% + ${(0.5 - frac) * THUMB_PX}px)` }}
            >
              {s.toLocaleString("en-US")}
            </button>
          );
        })}
      </div>
    </div>
  );
}
