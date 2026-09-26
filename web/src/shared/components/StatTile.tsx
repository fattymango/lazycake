import type { ReactNode } from "react";

export function StatTile({
  label,
  value,
  icon,
  tone = "default",
}: {
  label: string;
  value: string;
  icon?: (props: { className?: string }) => ReactNode;
  tone?: "default" | "good" | "warn";
}) {
  const toneClass = tone === "good" ? "text-good" : tone === "warn" ? "text-warn" : "text-text";
  return (
    <div className="flex-1 min-w-[140px] bg-panel border border-border rounded-xl2 shadow-panel p-4 flex items-start gap-3">
      {icon && (
        <div className="w-9 h-9 rounded-lg bg-panel2 border border-border flex items-center justify-center text-muted shrink-0">
          {icon({ className: "w-4.5 h-4.5" })}
        </div>
      )}
      <div className="min-w-0">
        <div className={`text-2xl font-semibold tabular-nums leading-tight ${toneClass}`}>{value}</div>
        <div className="text-xs text-muted mt-0.5">{label}</div>
      </div>
    </div>
  );
}

export function StatRow({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">{children}</div>;
}
