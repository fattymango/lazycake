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
  emptyLabel = "nothing here yet",
  onRowClick,
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  emptyLabel?: string;
  onRowClick?: (row: T) => void;
}) {
  if (rows.length === 0) return <EmptyState label={emptyLabel} />;

  return (
    <table className="w-full text-sm border-collapse">
      <thead>
        <tr>
          {columns.map((c) => (
            <th
              key={c.header}
              className="text-left text-muted font-medium text-[11px] uppercase tracking-wide px-2 py-1.5 border-b border-border"
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
            className={`border-b border-border last:border-none ${onRowClick ? "cursor-pointer hover:bg-white/5" : ""}`}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
          >
            {columns.map((c) => (
              <td key={c.header} className={`px-2 py-1.5 ${c.className ?? ""}`}>
                {c.render(row)}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
