import type { HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

/** Placeholder block with a soft shimmer; size it with className. */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      aria-hidden
      className={cn(
        "relative overflow-hidden rounded-md bg-raised",
        "after:absolute after:inset-0 after:-translate-x-full after:animate-shimmer after:bg-gradient-to-r after:from-transparent after:via-fg/[0.05] after:to-transparent",
        className
      )}
      {...props}
    />
  );
}
