export function LoadingState({ label = "loading…" }: { label?: string }) {
  return <div className="text-muted text-sm py-8 text-center">{label}</div>;
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="text-bad text-sm py-8 text-center">
      <div>{message}</div>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-3 text-xs text-accent border border-border rounded px-3 py-1 hover:border-accent"
        >
          retry
        </button>
      )}
    </div>
  );
}

export function EmptyState({ label }: { label: string }) {
  return <div className="text-muted text-sm py-8 text-center font-mono">{label}</div>;
}
