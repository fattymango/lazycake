import type { ReactNode } from "react";

export function Panel({ title, children, className = "" }: { title?: string; children: ReactNode; className?: string }) {
  return (
    <div className={`bg-panel border border-border rounded-lg p-4 ${className}`}>
      {title && <h2 className="text-xs uppercase tracking-wide text-muted mb-3">{title}</h2>}
      {children}
    </div>
  );
}
