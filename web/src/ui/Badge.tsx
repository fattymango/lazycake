import type { HTMLAttributes } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/cn";
import { toneClasses, type Tone } from "./tone";

const badgeVariants = cva("inline-flex max-w-full items-center gap-1.5 whitespace-nowrap rounded-full border font-medium", {
  variants: {
    size: { sm: "h-5 px-2 text-2xs", md: "h-6 px-2.5 text-xs" },
  },
  defaultVariants: { size: "md" },
});

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {
  tone?: Tone;
  /** Leading status dot. */
  dot?: boolean;
  /** Animate the dot (something is actively happening). */
  pulse?: boolean;
}

export function Badge({ className, tone = "neutral", size, dot, pulse, children, ...props }: BadgeProps) {
  const t = toneClasses[tone];
  return (
    <span className={cn(badgeVariants({ size }), t.bg, t.text, t.border, className)} {...props}>
      {dot && (
        <span className="relative flex size-1.5 shrink-0">
          {pulse && <span className={cn("absolute inline-flex size-full animate-ping-soft rounded-full opacity-70", t.dot)} />}
          <span className={cn("relative inline-flex size-1.5 rounded-full", t.dot)} />
        </span>
      )}
      <span className="truncate">{children}</span>
    </span>
  );
}
