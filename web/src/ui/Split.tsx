import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

/**
 * The page layout for anything with a main subject and supporting detail: the subject (a table, a chart, a log, a form) takes the
 * left and all the room there is, and stats and secondary cards stack in a rail on the right. Below 1400px the rail drops under the
 * main column, so a phone or a narrow window still reads top to bottom.
 */
export function Split({
  main,
  side,
  stickySide,
  className,
}: {
  main: ReactNode;
  side: ReactNode;
  /** Keep the rail in view while the main column scrolls (a form's summary, a log's metadata). */
  stickySide?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn("grid items-start gap-6 min-[1400px]:grid-cols-[minmax(0,1fr)_23rem] 2xl:grid-cols-[minmax(0,1fr)_27rem]", className)}
    >
      <div className="min-w-0 space-y-6">{main}</div>
      <aside className={cn("min-w-0 space-y-4", stickySide && "min-[1400px]:sticky min-[1400px]:top-20")}>{side}</aside>
    </div>
  );
}

/** Stat tiles for a rail: two across, compact. */
export function StatRail({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("grid grid-cols-2 gap-3", className)}>{children}</div>;
}
