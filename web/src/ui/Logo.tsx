import { cn } from "@/lib/cn";

/**
 * The LazyCake mark: a chocolate cake that is also a database server. Source art in docs/brand; public/logo.png (cream tile) and
 * public/logo-dark.png (same art on the dark theme's surface colour) are the 128 px exports, one shown per theme.
 */
export function LogoMark({ className }: { className?: string }) {
  const size = cn("size-8 shrink-0", className);
  return (
    <>
      <img src="/logo.png" alt="" aria-hidden className={cn(size, "dark:hidden")} />
      <img src="/logo-dark.png" alt="" aria-hidden className={cn(size, "hidden dark:block")} />
    </>
  );
}

export function Logo({ className, compact }: { className?: string; compact?: boolean }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <LogoMark className="size-7" />
      {!compact && <span className="text-[0.9375rem] font-semibold tracking-tight text-fg">LazyCake</span>}
    </span>
  );
}
