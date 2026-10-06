import * as RadixTooltip from "@radix-ui/react-tooltip";
import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

/** Mount once near the root so every tooltip shares timing. */
export const TooltipProvider = ({ children }: { children: ReactNode }) => (
  <RadixTooltip.Provider delayDuration={250} skipDelayDuration={150}>
    {children}
  </RadixTooltip.Provider>
);

export function Tooltip({
  content,
  children,
  side = "top",
  className,
  disabled,
}: {
  content: ReactNode;
  children: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  className?: string;
  disabled?: boolean;
}) {
  if (disabled || content === null || content === undefined || content === "") return <>{children}</>;
  return (
    <RadixTooltip.Root>
      <RadixTooltip.Trigger asChild>{children}</RadixTooltip.Trigger>
      <RadixTooltip.Portal>
        <RadixTooltip.Content
          side={side}
          sideOffset={6}
          collisionPadding={12}
          className={cn(
            "z-[60] max-w-[min(26rem,calc(100vw-1.5rem))] animate-fade-in break-all rounded-lg border border-border bg-overlay px-2.5 py-1.5 text-xs leading-5 text-fg shadow-pop",
            className
          )}
        >
          {content}
        </RadixTooltip.Content>
      </RadixTooltip.Portal>
    </RadixTooltip.Root>
  );
}
