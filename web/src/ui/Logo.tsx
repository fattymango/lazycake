import { cn } from "@/lib/cn";

/** The LazyCake mark. Same artwork as public/favicon.svg. */
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden className={cn("size-8 shrink-0", className)}>
      <defs>
        <linearGradient id="lc-logo" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#8f96ff" />
          <stop offset="1" stopColor="#5646ee" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="9" fill="url(#lc-logo)" />
      <path d="M7.5 21 16 9.5 24.5 21v2a1.5 1.5 0 0 1-1.5 1.5H9A1.5 1.5 0 0 1 7.5 23z" fill="#fff" fillOpacity=".96" />
      <path d="M7.7 18.4h16.6" stroke="#6657f2" strokeWidth="1.7" />
      <circle cx="16" cy="8.4" r="1.9" fill="#fff" />
    </svg>
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
