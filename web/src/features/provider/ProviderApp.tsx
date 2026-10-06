import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { LayoutDashboard, PlusCircle, Wallet } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { AppShell, type NavSection } from "@/ui/AppShell";
import { ErrorBoundary } from "@/ui/ErrorBoundary";
import { NotFound } from "@/ui/NotFound";
import { AddMachine } from "./AddMachine";
import { Earnings } from "./Earnings";
import { MachineDetail } from "./MachineDetail";
import { Overview } from "./Overview";

const nav: NavSection[] = [
  {
    label: "Fleet",
    items: [
      { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
      { to: "/machines/add", label: "Add machine", icon: PlusCircle },
    ],
  },
  { label: "Account", items: [{ to: "/earnings", label: "Earnings", icon: Wallet }] },
];

const crumbs = {
  machines: { label: "Machines", to: "/" },
  "/machines/add": { label: "Add machine" },
  earnings: { label: "Earnings" },
};

export function ProviderApp() {
  const { me, logout } = useAuth();
  const { pathname } = useLocation();
  return (
    <AppShell nav={nav} crumbs={crumbs} roleLabel="Provider" username={me?.username ?? ""} onLogout={logout}>
      <ErrorBoundary key={pathname} inline>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/machines/add" element={<AddMachine />} />
          <Route path="/machines/:id" element={<MachineDetail />} />
          <Route path="/earnings" element={<Earnings />} />
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </ErrorBoundary>
    </AppShell>
  );
}
