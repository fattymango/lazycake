import type { ReactNode } from "react";
import { AlertTriangle, Inbox } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/cn";
import { Button } from "./Button";

export function EmptyState({
  icon: Icon = Inbox,
  title,
  description,
  action,
  compact,
  className,
}: {
  icon?: LucideIcon;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  compact?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 text-center", compact ? "py-8" : "py-14", className)}>
      <div className="mb-4 flex size-11 items-center justify-center rounded-xl border border-border bg-raised text-muted shadow-sm">
        <Icon className="size-5" aria-hidden />
      </div>
      <h3 className="text-sm font-semibold text-fg">{title}</h3>
      {description && <p className="mt-1.5 max-w-sm text-sm leading-5 text-muted">{description}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

export function ErrorState({
  title = "Something went wrong",
  message,
  onRetry,
  compact,
  className,
}: {
  title?: ReactNode;
  message?: ReactNode;
  onRetry?: () => void;
  compact?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 text-center", compact ? "py-8" : "py-14", className)} role="alert">
      <div className="mb-4 flex size-11 items-center justify-center rounded-xl border border-danger/25 bg-danger/10 text-danger">
        <AlertTriangle className="size-5" aria-hidden />
      </div>
      <h3 className="text-sm font-semibold text-fg">{title}</h3>
      {message && <p className="mt-1.5 max-w-md break-words text-sm leading-5 text-muted">{message}</p>}
      {onRetry && (
        <Button variant="secondary" size="sm" className="mt-5" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}
