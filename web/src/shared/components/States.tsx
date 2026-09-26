import type { ReactNode } from "react";
import { IconAlertTriangle, IconInbox } from "./Icon";

export function LoadingState({ label = "Loading…" }: { label?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-14 text-muted text-sm">
      <div className="w-6 h-6 rounded-full border-2 border-border border-t-accent animate-spin" />
      {label}
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-14 text-center">
      <div className="w-10 h-10 rounded-full bg-bad/10 border border-bad/30 flex items-center justify-center text-bad">
        <IconAlertTriangle className="w-5 h-5" />
      </div>
      <div className="text-sm text-text max-w-sm">{message}</div>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-1 text-xs font-medium text-accent border border-border rounded-lg px-3 py-1.5 hover:border-accent transition-colors"
        >
          Retry
        </button>
      )}
    </div>
  );
}

// A real empty state - icon, a specific message, and (when there's a next
// action) a button to take it - not just "nothing here yet" in muted
// monospace, which read as unfinished rather than intentionally empty.
export function EmptyState({
  label,
  icon,
  action,
}: {
  label: string;
  icon?: (props: { className?: string }) => ReactNode;
  action?: ReactNode;
}) {
  const Icon = icon ?? IconInbox;
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-14 text-center">
      <div className="w-11 h-11 rounded-xl bg-panel2 border border-border flex items-center justify-center text-muted">
        <Icon className="w-5 h-5" />
      </div>
      <div className="text-sm text-muted max-w-sm">{label}</div>
      {action}
    </div>
  );
}
