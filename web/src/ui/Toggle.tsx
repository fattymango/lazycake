import type { ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/cn";

/** A pressed/unpressed toolbar button (wrap lines, follow output...). */
export function Toggle({ pressed, className, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { pressed: boolean }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs font-medium transition-colors [&_svg]:size-3.5",
        pressed ? "border-accent/30 bg-accent/10 text-accent" : "border-transparent text-muted hover:bg-raised hover:text-fg",
        className
      )}
      {...props}
    />
  );
}
