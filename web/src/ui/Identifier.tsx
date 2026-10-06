import { useLayoutEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { CopyButton } from "./CopyButton";
import { Tooltip } from "./Tooltip";

/**
 * "tsk_01M4170DCF1506FFA2A58939F6" -> "tsk_01M4…8939F6". Keeps the type
 * prefix and the end of the ID (the part that actually differs between two
 * IDs) and drops the middle, so a column of IDs stays readable and narrow.
 */
export function shortenId(value: string, head = 4, tail = 6): string {
  const m = /^([a-z]{2,5}_)(.+)$/.exec(value);
  const [prefix, body] = m ? [m[1], m[2]] : ["", value];
  if (body.length <= head + tail + 1) return value;
  return `${prefix}${body.slice(0, head)}…${body.slice(-tail)}`;
}

/**
 * A machine identifier (task/node/gateway ID, digest, key): monospaced,
 * shortened in the middle, full value on hover, one-click copy. It can never
 * overflow its container: even the shortened text truncates if the box is
 * narrower still.
 */
export function Identifier({
  value,
  head,
  tail,
  copy = true,
  className,
  textClassName,
  full,
}: {
  value: string;
  head?: number;
  tail?: number;
  copy?: boolean;
  className?: string;
  /** Styles for the text itself (size, color), e.g. when used as a page title. */
  textClassName?: string;
  /** Show the whole value (still truncating at the container edge). */
  full?: boolean;
}) {
  const text = full ? value : shortenId(value, head, tail);
  return (
    <span className={cn("group/id inline-flex min-w-0 max-w-full items-center gap-0.5", className)}>
      <Tooltip content={text !== value || full ? <span className="font-mono">{value}</span> : null}>
        <span className={cn("min-w-0 truncate font-mono text-[0.8125rem] leading-5", textClassName)} data-testid="identifier">
          {text}
        </span>
      </Tooltip>
      {copy && (
        <CopyButton
          value={value}
          label="Copy ID"
          className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover/id:opacity-100 [@media(hover:none)]:opacity-100"
        />
      )}
    </span>
  );
}

/** Single-line text that ends in an ellipsis instead of overflowing, with the full text on hover when (and only when) it was cut. */
export function Truncate({ children, className, title }: { children: ReactNode; className?: string; title?: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const [cut, setCut] = useState(false);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setCut(el.scrollWidth > el.clientWidth + 1);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [children]);

  const text = title ?? (typeof children === "string" ? children : undefined);
  return (
    <Tooltip content={cut ? text : null}>
      <span ref={ref} className={cn("block min-w-0 truncate", className)}>
        {children}
      </span>
    </Tooltip>
  );
}
