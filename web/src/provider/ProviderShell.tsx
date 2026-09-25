import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { NavShell } from "@shared/components/NavShell";
import { useProviderAuth } from "@shared/auth";
import { useSSE } from "@shared/hooks/useSSE";

export function ProviderShell({ children }: { children: ReactNode }) {
  const { me, logout } = useProviderAuth();
  const navigate = useNavigate();
  const live = useSSE(me ? "/api/portal/provider/events" : null, () => {});

  async function handleLogout() {
    await logout();
    navigate("/login");
  }

  return (
    <NavShell
      title="LazyCake"
      username={me?.username}
      onLogout={handleLogout}
      live={live}
      items={[
        { to: "/", label: "Dashboard" },
        { to: "/machines/add", label: "Add a machine" },
        { to: "/earnings", label: "Earnings" },
      ]}
    >
      {children}
    </NavShell>
  );
}
