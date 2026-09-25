import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";

export interface NavItem {
  to: string;
  label: string;
}

// Sidebar nav + top bar + content, per PLAN.md §6's visual direction: clean
// and minimal, one shell shared by both portals with a different NavItem
// list and title.
export function NavShell({
  title,
  items,
  username,
  onLogout,
  live,
  children,
}: {
  title: string;
  items: NavItem[];
  username?: string;
  onLogout: () => void;
  live?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="min-h-screen flex flex-col md:flex-row">
      <aside className="md:w-56 shrink-0 border-b md:border-b-0 md:border-r border-border bg-panel p-4 flex md:flex-col gap-2">
        <div className="font-semibold text-lg mb-0 md:mb-6">{title}</div>
        <nav className="flex md:flex-col gap-1 flex-wrap md:flex-nowrap">
          {items.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                `text-sm px-3 py-2 rounded transition-colors ${
                  isActive ? "bg-accent/15 text-accent" : "text-muted hover:text-text"
                }`
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        <header className="flex items-center justify-between gap-4 px-6 py-3 border-b border-border">
          <div className="flex items-center gap-2 text-xs text-muted">
            <span className={`inline-block w-2 h-2 rounded-full ${live ? "bg-good" : "bg-bad"}`} />
            {live ? "live" : "reconnecting…"}
          </div>
          <div className="flex items-center gap-4 text-sm">
            {username && <span className="text-muted">{username}</span>}
            <button onClick={onLogout} className="text-muted hover:text-text">
              log out
            </button>
          </div>
        </header>
        <main className="flex-1 p-6 max-w-5xl w-full mx-auto">{children}</main>
      </div>
    </div>
  );
}
