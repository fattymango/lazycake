import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import * as RadixDialog from "@radix-ui/react-dialog";
import { ChevronRight, LogOut, Menu, PanelLeftClose, PanelLeftOpen, WifiOff } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/cn";
import { useLiveStatus } from "@/lib/live";
import { shortenId } from "./Identifier";
import { Badge } from "./Badge";
import { Button } from "./Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "./DropdownMenu";
import { Logo } from "./Logo";
import { ThemeToggle } from "./ThemeToggle";
import { Tooltip } from "./Tooltip";

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  /** Match this path exactly (use for "/"). */
  end?: boolean;
}

export interface NavSection {
  label?: string;
  items: NavItem[];
}

export interface AppShellProps {
  nav: NavSection[];
  /** Breadcrumb label per first path segment (e.g. tasks -> "Tasks"). Unknown segments are shown as-is. */
  crumbs: Record<string, { label: string; to?: string }>;
  roleLabel: string;
  username: string;
  onLogout: () => void;
  /** Extra controls in the top bar, left of the theme toggle (e.g. a balance chip). */
  topbarExtra?: ReactNode;
  children: ReactNode;
}

const COLLAPSE_KEY = "lc-sidebar-collapsed";

function useCollapsed(): [boolean, () => void] {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSE_KEY) === "1";
    } catch {
      return false;
    }
  });
  const toggle = () =>
    setCollapsed((c) => {
      try {
        localStorage.setItem(COLLAPSE_KEY, c ? "0" : "1");
      } catch {
        // preference just isn't remembered
      }
      return !c;
    });
  return [collapsed, toggle];
}

function NavList({ nav, collapsed, onNavigate }: { nav: NavSection[]; collapsed?: boolean; onNavigate?: () => void }) {
  return (
    <nav aria-label="Main" className="flex-1 space-y-6 overflow-y-auto px-3 py-2">
      {nav.map((section, i) => (
        <div key={i}>
          {section.label && !collapsed && (
            <p className="mb-1.5 px-2.5 text-2xs font-medium uppercase tracking-wider text-subtle">{section.label}</p>
          )}
          {section.label && collapsed && i > 0 && <div className="mx-2 mb-2 h-px bg-border" />}
          <ul className="space-y-0.5">
            {section.items.map((item) => (
              <li key={item.to}>
                <Tooltip content={collapsed ? item.label : null} side="right">
                  <NavLink
                    to={item.to}
                    end={item.end}
                    onClick={onNavigate}
                    aria-label={collapsed ? item.label : undefined}
                    className={({ isActive }) =>
                      cn(
                        "group relative flex h-9 items-center gap-3 rounded-lg px-2.5 text-sm font-medium transition-colors",
                        collapsed && "justify-center px-0",
                        isActive ? "bg-raised text-fg" : "text-muted hover:bg-raised/60 hover:text-fg"
                      )
                    }
                  >
                    {({ isActive }) => (
                      <>
                        {isActive && <span className="absolute -left-3 top-2 h-5 w-[3px] rounded-r-full bg-accent" aria-hidden />}
                        <item.icon
                          className={cn("size-[1.125rem] shrink-0", isActive ? "text-accent" : "text-muted group-hover:text-fg")}
                          aria-hidden
                        />
                        {!collapsed && <span className="truncate">{item.label}</span>}
                      </>
                    )}
                  </NavLink>
                </Tooltip>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}

function Breadcrumbs({ crumbs }: { crumbs: AppShellProps["crumbs"] }) {
  const { pathname } = useLocation();
  const segments = pathname.split("/").filter(Boolean);
  const trail: { label: string; to?: string }[] = [];
  let path = "";
  segments.forEach((seg, i) => {
    path += `/${seg}`;
    const known = crumbs[path] ?? (i === 0 ? crumbs[seg] : undefined);
    const isId = /^[a-z]{2,5}_/.test(seg);
    const label = known?.label ?? (isId ? shortenId(seg) : seg.charAt(0).toUpperCase() + seg.slice(1).replace(/-/g, " "));
    trail.push({ label, to: i === segments.length - 1 ? undefined : (known?.to ?? path) });
  });
  if (trail.length === 0) trail.push({ label: "Overview" });

  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="flex min-w-0 items-center gap-1.5 text-sm">
        {trail.map((c, i) => (
          <li key={i} className="flex min-w-0 items-center gap-1.5">
            {i > 0 && <ChevronRight className="size-3.5 shrink-0 text-subtle" aria-hidden />}
            {c.to ? (
              <Link to={c.to} className="truncate text-muted transition-colors hover:text-fg">
                {c.label}
              </Link>
            ) : (
              <span
                className={cn("truncate font-medium text-fg", /^[a-z]{2,5}_/.test(segments[i] ?? "") && "font-mono text-[0.8125rem]")}
                aria-current="page"
              >
                {c.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

function LiveIndicator() {
  const status = useLiveStatus();
  const tone = status === "live" ? "success" : status === "connecting" ? "neutral" : "warning";
  const label = status === "live" ? "Live" : status === "connecting" ? "Connecting" : "Reconnecting";
  return (
    <Tooltip
      content={
        status === "live"
          ? "Receiving live updates"
          : status === "connecting"
            ? "Connecting to live updates"
            : "Live updates paused; reconnecting"
      }
    >
      <span className="hidden sm:inline-flex">
        <Badge tone={tone} dot pulse={status === "live"} size="sm">
          {label}
        </Badge>
      </span>
    </Tooltip>
  );
}

function useOnline() {
  const [online, setOnline] = useState(() => navigator.onLine);
  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    window.addEventListener("online", on);
    window.addEventListener("offline", off);
    return () => {
      window.removeEventListener("online", on);
      window.removeEventListener("offline", off);
    };
  }, []);
  return online;
}

function UserMenu({ username, roleLabel, onLogout }: { username: string; roleLabel: string; onLogout: () => void }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className="flex h-9 items-center gap-2.5 rounded-lg px-1.5 transition-colors hover:bg-raised data-[state=open]:bg-raised"
          aria-label="Account menu"
        >
          <span className="flex size-7 items-center justify-center rounded-full bg-gradient-to-br from-accent/90 to-accent/50 text-xs font-semibold text-accent-fg">
            {username.slice(0, 1).toUpperCase()}
          </span>
          <span className="hidden max-w-[9rem] truncate text-sm font-medium text-fg md:block">{username}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-60">
        <DropdownMenuLabel>Signed in as</DropdownMenuLabel>
        <div className="px-2.5 pb-2">
          <p className="truncate text-sm font-medium text-fg">{username}</p>
          <p className="text-xs text-muted">{roleLabel} account</p>
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<LogOut />} onSelect={onLogout}>
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Pages are held to a readable width by default. A page that is mostly controls and side-by-side panels
 * (rather than prose or a table) can ask for the wide layout, which lets it use a big screen:
 * `useWideLayout()` at the top of the page.
 */
const WideLayoutContext = createContext<(wide: boolean) => void>(() => {});

export function useWideLayout() {
  const setWide = useContext(WideLayoutContext);
  useEffect(() => {
    setWide(true);
    return () => setWide(false);
  }, [setWide]);
}

export function AppShell({ nav, crumbs, roleLabel, username, onLogout, topbarExtra, children }: AppShellProps) {
  const [collapsed, toggleCollapsed] = useCollapsed();
  const [drawer, setDrawer] = useState(false);
  const online = useOnline();
  const [wide, setWide] = useState(false);
  const { pathname } = useLocation();

  useEffect(() => setDrawer(false), [pathname]);

  return (
    <div className="min-h-screen bg-bg">
      {/* Desktop sidebar */}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-30 hidden flex-col border-r border-border bg-surface transition-[width] duration-200 lg:flex",
          collapsed ? "w-[4.5rem]" : "w-64"
        )}
      >
        <div
          className={cn("flex h-14 shrink-0 items-center border-b border-border", collapsed ? "justify-center" : "justify-between px-5")}
        >
          <Link to="/" aria-label="LazyCake home">
            <Logo compact={collapsed} />
          </Link>
          {!collapsed && (
            <Badge tone="accent" size="sm">
              {roleLabel}
            </Badge>
          )}
        </div>
        <NavList nav={nav} collapsed={collapsed} />
        <div className={cn("shrink-0 border-t border-border p-3", collapsed && "flex justify-center")}>
          <Tooltip content={collapsed ? "Expand sidebar" : null} side="right">
            <Button
              variant="ghost"
              size={collapsed ? "icon" : "sm"}
              onClick={toggleCollapsed}
              className={cn(!collapsed && "w-full justify-start text-muted")}
              aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            >
              {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
              {!collapsed && "Collapse"}
            </Button>
          </Tooltip>
        </div>
      </aside>

      {/* Mobile drawer */}
      <RadixDialog.Root open={drawer} onOpenChange={setDrawer}>
        <RadixDialog.Portal>
          <RadixDialog.Overlay className="fixed inset-0 z-50 animate-fade-in bg-black/60 lg:hidden" />
          <RadixDialog.Content className="fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85vw] animate-fade-in flex-col border-r border-border bg-surface shadow-pop focus:outline-none lg:hidden">
            <RadixDialog.Title className="sr-only">Navigation</RadixDialog.Title>
            <RadixDialog.Description className="sr-only">Site navigation</RadixDialog.Description>
            <div className="flex h-14 shrink-0 items-center justify-between border-b border-border px-5">
              <Logo />
              <Badge tone="accent" size="sm">
                {roleLabel}
              </Badge>
            </div>
            <NavList nav={nav} onNavigate={() => setDrawer(false)} />
          </RadixDialog.Content>
        </RadixDialog.Portal>
      </RadixDialog.Root>

      <div className={cn("transition-[padding] duration-200", collapsed ? "lg:pl-[4.5rem]" : "lg:pl-64")}>
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border bg-bg/80 px-4 backdrop-blur-md sm:px-6 lg:px-8">
          <Button variant="ghost" size="icon" className="-ml-2 lg:hidden" onClick={() => setDrawer(true)} aria-label="Open navigation">
            <Menu />
          </Button>
          <Link to="/" className="lg:hidden" aria-label="LazyCake home">
            <Logo compact />
          </Link>
          <div className="min-w-0 flex-1">
            <Breadcrumbs crumbs={crumbs} />
          </div>
          <div className="flex shrink-0 items-center gap-1.5 sm:gap-2">
            <LiveIndicator />
            {topbarExtra}
            <ThemeToggle />
            <UserMenu username={username} roleLabel={roleLabel} onLogout={onLogout} />
          </div>
        </header>

        {!online && (
          <div
            role="alert"
            className="flex items-center justify-center gap-2 border-b border-warning/25 bg-warning/10 px-4 py-2 text-xs text-warning"
          >
            <WifiOff className="size-3.5" aria-hidden />
            You're offline. Showing the last data we had; it will refresh when you reconnect.
          </div>
        )}

        <main id="main" className={cn("mx-auto w-full px-4 py-6 sm:px-6 lg:px-8 lg:py-8", wide ? "max-w-[87.5rem]" : "max-w-[75rem]")}>
          <WideLayoutContext.Provider value={setWide}>{children}</WideLayoutContext.Provider>
        </main>
      </div>
    </div>
  );
}
