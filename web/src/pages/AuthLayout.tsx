import type { ReactNode } from "react";

// Shared chrome for the login/signup pages - logo, card, footer link. Pulled
// out of the old per-role AuthForm so both pages (now one of each, not one
// per portal) look identical apart from their actual content.
export function AuthLayout({ title, subtitle, children, footer }: { title: string; subtitle: string; children: ReactNode; footer: ReactNode }) {
  return (
    <div className="min-h-screen flex items-center justify-center p-6 bg-bg text-text">
      <div className="max-w-sm w-full">
        <div className="flex items-center gap-2.5 justify-center mb-8">
          <div className="w-8 h-8 rounded-lg bg-accent/15 border border-accent/30 flex items-center justify-center">
            <div className="w-3 h-3 rounded-[3px] bg-accent" />
          </div>
          <span className="font-semibold text-lg tracking-tight">LazyCake</span>
        </div>

        <div className="bg-panel border border-border rounded-xl2 shadow-panel p-6">
          <h1 className="text-lg font-semibold mb-1">{title}</h1>
          <p className="text-muted text-sm mb-6">{subtitle}</p>
          {children}
        </div>

        <div className="mt-5 text-center text-sm text-muted">{footer}</div>
      </div>
    </div>
  );
}
