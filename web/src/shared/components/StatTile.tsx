import type { ReactNode } from "react";

export function StatTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex-1 min-w-[120px]">
      <div className="text-2xl font-semibold">{value}</div>
      <div className="text-xs text-muted">{label}</div>
    </div>
  );
}

export function StatRow({ children }: { children: ReactNode }) {
  return <div className="flex gap-6 flex-wrap">{children}</div>;
}
