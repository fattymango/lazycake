import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { Skeleton } from "./Skeleton";

export interface Column<T> {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** Fixed width ("8rem", "120px"); columns without one share what's left. */
  width?: string;
  align?: "left" | "right" | "center";
  /** Hide below this breakpoint. */
  hideBelow?: "sm" | "md" | "lg" | "xl";
  /** Cell text is allowed to wrap (default: cells are single-line and the content truncates itself). */
  wrap?: boolean;
}

const hide: Record<NonNullable<Column<unknown>["hideBelow"]>, string> = {
  sm: "max-sm:hidden",
  md: "max-md:hidden",
  lg: "max-lg:hidden",
  xl: "max-xl:hidden",
};

/**
 * Fixed-layout table: columns get their stated widths and the rest share
 * the space, so a cell can never stretch the table. Cell content is expected
 * to truncate itself (Identifier, Truncate); the table sits in a horizontal
 * scroller as a last resort rather than blowing out the page.
 */
export function DataTable<T>({
  columns,
  rows,
  getRowKey,
  onRowClick,
  loading,
  skeletonRows = 5,
  empty,
  caption,
  minWidth = 640,
  mobileCard,
  className,
}: {
  columns: Column<T>[];
  rows: T[];
  getRowKey: (row: T) => string;
  onRowClick?: (row: T) => void;
  loading?: boolean;
  skeletonRows?: number;
  empty?: ReactNode;
  /** Accessible name for the table. */
  caption: string;
  minWidth?: number;
  /** Below `sm`, render rows as these cards instead of a table. */
  mobileCard?: (row: T) => ReactNode;
  className?: string;
}) {
  const showEmpty = !loading && rows.length === 0 && empty;

  return (
    <div className={cn("min-w-0", className)}>
      {mobileCard && !loading && rows.length > 0 && (
        <ul className="divide-y divide-border sm:hidden">
          {rows.map((row) => (
            <li key={getRowKey(row)}>
              {onRowClick ? (
                <button
                  type="button"
                  onClick={() => onRowClick(row)}
                  className="block w-full px-4 py-3.5 text-left transition-colors hover:bg-raised/60"
                >
                  {mobileCard(row)}
                </button>
              ) : (
                <div className="px-4 py-3.5">{mobileCard(row)}</div>
              )}
            </li>
          ))}
        </ul>
      )}

      {showEmpty ? (
        empty
      ) : (
        <div data-scroll-x className={cn("overflow-x-auto", mobileCard && !loading && rows.length > 0 && "max-sm:hidden")}>
          <table className="w-full table-fixed border-collapse text-sm" style={{ minWidth }}>
            <caption className="sr-only">{caption}</caption>
            <colgroup>
              {columns.map((c) => (
                <col key={c.id} className={c.hideBelow ? hide[c.hideBelow] : undefined} style={c.width ? { width: c.width } : undefined} />
              ))}
            </colgroup>
            <thead>
              <tr className="border-b border-border bg-raised/40">
                {columns.map((c) => (
                  <th
                    key={c.id}
                    scope="col"
                    className={cn(
                      "px-4 py-2.5 text-left text-2xs font-medium uppercase tracking-wider text-muted",
                      c.align === "right" && "text-right",
                      c.align === "center" && "text-center",
                      c.hideBelow && hide[c.hideBelow]
                    )}
                  >
                    {c.header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {loading
                ? Array.from({ length: skeletonRows }, (_, i) => (
                    <tr key={i}>
                      {columns.map((c) => (
                        <td key={c.id} className={cn("px-4 py-3.5", c.hideBelow && hide[c.hideBelow])}>
                          <Skeleton className={cn("h-4", i % 2 ? "w-3/5" : "w-4/5")} />
                        </td>
                      ))}
                    </tr>
                  ))
                : rows.map((row) => (
                    <tr
                      key={getRowKey(row)}
                      tabIndex={onRowClick ? 0 : undefined}
                      onClick={onRowClick ? () => onRowClick(row) : undefined}
                      onKeyDown={
                        onRowClick
                          ? (e) => {
                              if (e.key === "Enter" && e.target === e.currentTarget) onRowClick(row);
                            }
                          : undefined
                      }
                      className={cn("transition-colors", onRowClick && "cursor-pointer hover:bg-raised/60 focus-visible:bg-raised/60")}
                    >
                      {columns.map((c) => (
                        <td
                          key={c.id}
                          className={cn(
                            "px-4 py-3 align-middle",
                            !c.wrap && "overflow-hidden",
                            c.align === "right" && "text-right",
                            c.align === "center" && "text-center",
                            c.hideBelow && hide[c.hideBelow]
                          )}
                        >
                          {c.cell(row)}
                        </td>
                      ))}
                    </tr>
                  ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
