import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

export interface KeyValueItem {
  label: ReactNode;
  value: ReactNode;
  /** Skip the row (e.g. a field that doesn't apply to this task). */
  hidden?: boolean;
}

/** Label/value pairs (a description list). Values wrap or truncate themselves; they never push the layout wider. */
export function KeyValueList({ items, className }: { items: KeyValueItem[]; className?: string }) {
  return (
    <dl className={cn("grid grid-cols-[minmax(5.5rem,8rem)_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm", className)}>
      {items
        .filter((i) => !i.hidden)
        .map((item, idx) => (
          <div key={idx} className="contents">
            <dt className="text-muted">{item.label}</dt>
            <dd className="min-w-0 break-words text-fg">{item.value}</dd>
          </div>
        ))}
    </dl>
  );
}
