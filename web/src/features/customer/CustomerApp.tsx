import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { CreditCard, LayoutDashboard, ListChecks, Network, SquarePlus, Wallet } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { useLiveEvents } from "@/lib/live";
import { useAsync } from "@/lib/hooks/useAsync";
import { apiGet } from "@/lib/api";
import { money } from "@/lib/format";
import type { Me } from "@/lib/types";
import { AppShell, type NavSection } from "@/ui/AppShell";
import { Button } from "@/ui/Button";
import { ErrorBoundary } from "@/ui/ErrorBoundary";
import { NotFound } from "@/ui/NotFound";
import { Tooltip } from "@/ui/Tooltip";
import { Billing } from "./Billing";
import { GatewayDetail } from "./GatewayDetail";
import { Gateways } from "./Gateways";
import { Overview } from "./Overview";
import { SubmitTask } from "./SubmitTask";
import { TaskDetail } from "./TaskDetail";
import { Tasks } from "./Tasks";

const nav: NavSection[] = [
  {
    label: "Workspace",
    items: [
      { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
      { to: "/tasks", label: "Tasks", icon: ListChecks, end: true },
      { to: "/tasks/new", label: "New task", icon: SquarePlus },
    ],
  },
  { label: "Infrastructure", items: [{ to: "/gateways", label: "Gateways", icon: Network }] },
  { label: "Account", items: [{ to: "/billing", label: "Billing", icon: CreditCard }] },
];

const crumbs = {
  tasks: { label: "Tasks" },
  "/tasks/new": { label: "New task" },
  gateways: { label: "Gateways" },
  billing: { label: "Billing" },
};

function BalanceChip() {
  const me = useAsync((s) => apiGet<Me>("/api/portal/customer/me", s), []);
  useLiveEvents((e) => {
    if (e.type === "balance")
      me.setData((cur) => (cur ? { ...cur, balance_micros: e.balance_micros, available_balance_micros: e.balance_micros } : cur));
    if (e.type === "task_state") me.reload();
  });
  if (!me.data) return null;
  return (
    <Tooltip content="Available balance. Click to manage billing.">
      <Button asChild variant="secondary" size="sm" className="hidden gap-1.5 font-medium sm:inline-flex">
        <Link to="/billing">
          <Wallet className="text-muted" />
          <span data-tnum>{money(me.data.available_balance_micros)}</span>
        </Link>
      </Button>
    </Tooltip>
  );
}

export function CustomerApp() {
  const { me, logout } = useAuth();
  const { pathname } = useLocation();
  return (
    <AppShell nav={nav} crumbs={crumbs} roleLabel="Customer" username={me?.username ?? ""} onLogout={logout} topbarExtra={<BalanceChip />}>
      <ErrorBoundary key={pathname} inline>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/tasks" element={<Tasks />} />
          <Route path="/tasks/new" element={<SubmitTask />} />
          <Route path="/tasks/:id" element={<TaskDetail />} />
          <Route path="/gateways" element={<Gateways />} />
          <Route path="/gateways/:id" element={<GatewayDetail />} />
          <Route path="/billing" element={<Billing />} />
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </ErrorBoundary>
    </AppShell>
  );
}
