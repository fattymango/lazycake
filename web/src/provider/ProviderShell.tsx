import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { NavShell } from "@shared/components/NavShell";
import { IconDashboard, IconEarnings, IconPlus } from "@shared/components/Icon";
import { useAuth } from "@shared/auth";
import { useSSE } from "@shared/hooks/useSSE";
import { useRoleMe } from "@shared/hooks/useRoleMe";
import { money } from "@shared/format";
import type { ProviderMe } from "@shared/types";

export function ProviderShell({ children }: { children: ReactNode }) {
  const { me, logout } = useAuth();
  const navigate = useNavigate();
  const { data: providerMe } = useRoleMe<ProviderMe>("/api/portal/provider/me");
  const live = useSSE(me ? "/api/portal/provider/events" : null, () => {});

  async function handleLogout() {
    await logout();
    navigate("/login");
  }

  return (
    <NavShell
      roleLabel="Provider"
      username={me?.username}
      onLogout={handleLogout}
      live={live}
      headerStat={{ label: "Lifetime earnings", value: money(providerMe?.lifetime_earnings_micros) }}
      items={[
        { to: "/", label: "Dashboard", icon: IconDashboard, end: true },
        { to: "/machines/add", label: "Add a machine", icon: IconPlus },
        { to: "/earnings", label: "Earnings", icon: IconEarnings },
      ]}
    >
      {children}
    </NavShell>
  );
}
