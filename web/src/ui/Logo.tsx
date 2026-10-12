import { cn } from "@/lib/cn";

/** The LazyCake mark: a chocolate cake that is also a database server. Source art in docs/brand; public/logo.png is the 128 px export. */
export function LogoMark({ className }: { className?: string }) {
  return <img src="/logo.png" alt="" aria-hidden className={cn("size-8 shrink-0", className)} />;
}

export function Logo({ className, compact }: { className?: string; compact?: boolean }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <LogoMark className="size-7" />
      {!compact && <span className="text-[0.9375rem] font-semibold tracking-tight text-fg">LazyCake</span>}
    </span>
  );
}
