import type { ReactNode } from "react";
import { AlertTriangle, CheckCircle2, Info, XCircle } from "lucide-react";
import { cn } from "@/lib/cn";
import { toneClasses, type Tone } from "./tone";

const icons: Record<Tone, typeof Info> = {
  neutral: Info,
  accent: Info,
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: XCircle,
};

/** Inline message that belongs to the content around it (not a toast). */
export function Alert({
  tone = "info",
  title,
  children,
  action,
  className,
}: {
  tone?: Tone;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  const t = toneClasses[tone];
  const Icon = icons[tone];
  return (
    <div
      role={tone === "danger" ? "alert" : "status"}
      className={cn("flex min-w-0 items-start gap-3 rounded-xl border p-3.5", t.bg, t.border, className)}
    >
      <Icon className={cn("mt-0.5 size-4 shrink-0", t.text)} aria-hidden />
      <div className="min-w-0 flex-1 text-sm leading-5">
        {title && <p className="font-medium text-fg">{title}</p>}
        {children && <div className={cn("break-words text-muted", title && "mt-0.5")}>{children}</div>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}
