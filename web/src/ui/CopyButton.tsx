import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/cn";
import { copyToClipboard } from "@/lib/clipboard";
import { Tooltip } from "./Tooltip";

/** Icon button that copies `value` and confirms with a check mark. */
export function CopyButton({
  value,
  label = "Copy",
  className,
  size = "sm",
  onCopied,
}: {
  value: string;
  label?: string;
  className?: string;
  size?: "sm" | "md";
  onCopied?: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number>();
  useEffect(() => () => window.clearTimeout(timer.current), []);

  async function copy(e: React.MouseEvent) {
    e.stopPropagation(); // rows are often clickable
    e.preventDefault();
    if (await copyToClipboard(value)) {
      setCopied(true);
      onCopied?.();
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1600);
    }
  }

  const Icon = copied ? Check : Copy;
  return (
    <Tooltip content={copied ? "Copied" : label}>
      <button
        type="button"
        onClick={copy}
        aria-label={copied ? "Copied" : label}
        className={cn(
          "inline-flex shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-fg",
          size === "sm" ? "size-6 [&_svg]:size-3.5" : "size-8 [&_svg]:size-4",
          copied && "text-success hover:text-success",
          className
        )}
      >
        <Icon aria-hidden />
      </button>
    </Tooltip>
  );
}
