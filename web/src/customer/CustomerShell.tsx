import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { NavShell } from "@shared/components/NavShell";
import { useCustomerAuth } from "@shared/auth";
import { useSSE } from "@shared/hooks/useSSE";

export function CustomerShell({ children }: { children: ReactNode }) {
  const { me, logout } = useCustomerAuth();
  const navigate = useNavigate();
  // A live dot in the nav is enough signal here; individual pages (task
  // detail, dashboard) subscribe to the same stream themselves for the
  // data they actually need to react to.
  const live = useSSE(me ? "/api/portal/customer/events" : null, () => {});

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
        { to: "/tasks/new", label: "Submit task" },
        { to: "/tasks", label: "Tasks" },
        { to: "/gateways", label: "Gateways" },
        { to: "/billing", label: "Billing" },
      ]}
    >
      {children}
    </NavShell>
  );
}
