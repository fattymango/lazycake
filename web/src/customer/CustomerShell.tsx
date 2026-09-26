import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { NavShell } from "@shared/components/NavShell";
import { IconBilling, IconDashboard, IconGateway, IconHistory, IconPlus } from "@shared/components/Icon";
import { useAuth } from "@shared/auth";
import { useSSE } from "@shared/hooks/useSSE";
import { useRoleMe } from "@shared/hooks/useRoleMe";
import { money } from "@shared/format";
import type { Me } from "@shared/types";

export function CustomerShell({ children }: { children: ReactNode }) {
  const { me, logout } = useAuth();
  const navigate = useNavigate();
  const { data: customerMe } = useRoleMe<Me>("/api/portal/customer/me");
  const live = useSSE(me ? "/api/portal/customer/events" : null, () => {});

  async function handleLogout() {
    await logout();
    navigate("/login");
  }

  return (
    <NavShell
      roleLabel="Customer"
      username={me?.username}
      onLogout={handleLogout}
      live={live}
      headerStat={{ label: "Available balance", value: money(customerMe?.available_balance_micros) }}
      items={[
        { to: "/", label: "Dashboard", icon: IconDashboard, end: true },
        { to: "/tasks/new", label: "Submit task", icon: IconPlus },
        { to: "/tasks", label: "Tasks", icon: IconHistory },
        { to: "/gateways", label: "Gateways", icon: IconGateway },
        { to: "/billing", label: "Billing", icon: IconBilling },
      ]}
    >
      {children}
    </NavShell>
  );
}
