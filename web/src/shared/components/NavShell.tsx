import { useState } from "react";
import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { IconChevronDown, IconLogout } from "./Icon";

export interface NavItem {
  to: string;
  label: string;
  icon: (props: { className?: string }) => ReactNode;
  end?: boolean;
}

function Logo() {
  return (
    <div className="flex items-center gap-2.5">
      <div className="w-7 h-7 rounded-lg bg-accent/15 border border-accent/30 flex items-center justify-center shrink-0">
        <div className="w-2.5 h-2.5 rounded-[3px] bg-accent" />
      </div>
      <span className="font-semibold tracking-tight text-[15px]">LazyCake</span>
    </div>
  );
}

// Sidebar nav + top bar + content - one shell shared by both portals (a
// role badge and a different NavItem list is the only thing that varies),
// per PLAN.md §6's "clean and minimal" direction, redesigned to actually
// look like a real product rather than a status page: a real nav (icons,
// active-state left rail), a header that carries the one number that
// matters for this role (balance or lifetime earnings) at a glance, and a
// proper account menu instead of a bare logout link.
export function NavShell({
  roleLabel,
  items,
  username,
  headerStat,
  onLogout,
  live,
  children,
}: {
  roleLabel: string;
  items: NavItem[];
  username?: string;
  headerStat?: { label: string; value: string };
  onLogout: () => void;
  live?: boolean;
  children: ReactNode;
}) {
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <div className="min-h-screen flex flex-col md:flex-row bg-bg">
      <aside className="md:w-60 shrink-0 border-b md:border-b-0 md:border-r border-border bg-panel flex md:flex-col">
        <div className="p-4 md:p-5 md:pb-3 flex items-center justify-between md:block">
          <Logo />
          <span className="hidden md:inline-flex mt-3 text-[10px] font-medium uppercase tracking-wider text-accent/90 bg-accent/10 border border-accent/20 rounded-full px-2 py-0.5">
            {roleLabel}
          </span>
        </div>
        <nav className="flex-1 px-2 md:px-3 pb-3 flex md:flex-col gap-1 flex-wrap md:flex-nowrap overflow-x-auto">
          {items.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                `group relative flex items-center gap-2.5 text-sm px-3 py-2 rounded-lg transition-colors whitespace-nowrap ${
                  isActive ? "bg-accent/10 text-accent" : "text-muted hover:text-text hover:bg-white/[0.03]"
                }`
              }
            >
              {({ isActive }) => (
                <>
                  {isActive && (
                    <span className="hidden md:block absolute left-0 top-1.5 bottom-1.5 w-[2.5px] rounded-full bg-accent" />
                  )}
                  <item.icon className="w-4 h-4 shrink-0" />
                  {item.label}
                </>
              )}
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        <header className="flex items-center justify-between gap-4 px-5 md:px-8 h-14 border-b border-border bg-bg/80 backdrop-blur sticky top-0 z-10">
          <div className="flex items-center gap-2 text-xs text-muted">
            <span className={`inline-block w-1.5 h-1.5 rounded-full ${live ? "bg-good" : "bg-bad animate-pulse"}`} />
            {live ? "live" : "reconnecting…"}
          </div>
          <div className="flex items-center gap-3 md:gap-5">
            {headerStat && (
              <div className="text-right leading-tight hidden sm:block">
                <div className="text-sm font-semibold tabular-nums">{headerStat.value}</div>
                <div className="text-[11px] text-muted">{headerStat.label}</div>
              </div>
            )}
            <div className="relative">
              <button
                onClick={() => setMenuOpen((v) => !v)}
                onBlur={() => setTimeout(() => setMenuOpen(false), 120)}
                className="flex items-center gap-2 pl-1 pr-2 py-1 rounded-lg hover:bg-white/[0.04] transition-colors"
              >
                <div className="w-6 h-6 rounded-full bg-panel2 border border-border flex items-center justify-center text-[11px] font-medium">
                  {username?.[0]?.toUpperCase() ?? "?"}
                </div>
                <span className="text-sm text-text hidden sm:inline">{username}</span>
                <IconChevronDown className="w-3.5 h-3.5 text-muted" />
              </button>
              {menuOpen && (
                <div className="absolute right-0 mt-2 w-44 bg-panel border border-border rounded-lg shadow-popover overflow-hidden">
                  <button
                    onClick={onLogout}
                    className="w-full flex items-center gap-2 px-3 py-2.5 text-sm text-text hover:bg-white/[0.04] transition-colors"
                  >
                    <IconLogout className="w-4 h-4 text-muted" />
                    Log out
                  </button>
                </div>
              )}
            </div>
          </div>
        </header>
        <main className="flex-1 p-5 md:p-8 max-w-6xl w-full mx-auto">{children}</main>
      </div>
    </div>
  );
}
