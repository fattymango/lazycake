import type { ReactNode } from "react";
import { EmptyState } from "./States";

export interface Column<T> {
  header: string;
  render: (row: T) => ReactNode;
  className?: string;
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  emptyLabel = "Nothing here yet.",
  emptyIcon,
  emptyAction,
  onRowClick,
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  emptyLabel?: string;
  emptyIcon?: (props: { className?: string }) => ReactNode;
  emptyAction?: ReactNode;
  onRowClick?: (row: T) => void;
}) {
  if (rows.length === 0) return <EmptyState label={emptyLabel} icon={emptyIcon} action={emptyAction} />;

  return (
    <div className="overflow-x-auto -mx-1">
      <table className="w-full text-sm border-collapse">
        <thead>
          <tr>
            {columns.map((c) => (
              <th
                key={c.header}
                className="text-left text-muted font-medium text-[11px] uppercase tracking-wide px-3 py-2 border-b border-border"
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={rowKey(row)}
              className={`border-b border-borderSoft last:border-none transition-colors ${
                onRowClick ? "cursor-pointer hover:bg-white/[0.03]" : ""
              }`}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
            >
              {columns.map((c) => (
                <td key={c.header} className={`px-3 py-2.5 ${c.className ?? ""}`}>
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
