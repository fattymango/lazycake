import { cn } from "@/lib/cn";
import { CopyButton } from "./CopyButton";

/** Preformatted, copyable text (commands, tokens, config). Scrolls sideways instead of overflowing. */
export function CodeBlock({
  code,
  label,
  wrap,
  className,
  copyLabel = "Copy",
}: {
  code: string;
  label?: string;
  /** Wrap long lines instead of scrolling. */
  wrap?: boolean;
  className?: string;
  copyLabel?: string;
}) {
  return (
    <div className={cn("relative min-w-0 rounded-lg border border-border bg-raised/70", className)}>
      {label && (
        <div className="flex items-center justify-between border-b border-border px-3.5 py-1.5">
          <span className="text-2xs font-medium uppercase tracking-wider text-muted">{label}</span>
        </div>
      )}
      {/* The copy button lives in its own column so scrolled code never slides under it. */}
      <div className="flex min-w-0 items-stretch">
        <pre
          data-scroll-x
          tabIndex={0}
          className={cn(
            "min-w-0 flex-1 p-3.5 font-mono text-[0.8125rem] leading-6 text-fg",
            wrap ? "whitespace-pre-wrap break-all" : "overflow-x-auto"
          )}
        >
          <code>{code}</code>
        </pre>
        <div className="flex shrink-0 items-start border-l border-border p-1.5">
          <CopyButton value={code} label={copyLabel} />
        </div>
      </div>
    </div>
  );
}
