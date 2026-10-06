import { RequireAuth, useAuth } from "@/lib/auth";
import { LiveProvider } from "@/lib/live";
import { CustomerApp } from "./customer/CustomerApp";
import { ProviderApp } from "./provider/ProviderApp";

/** Signed-in area: picks the customer or provider app from the account's own role. */
export function Portal() {
  return (
    <RequireAuth>
      <RoleApp />
    </RequireAuth>
  );
}

function RoleApp() {
  const { me } = useAuth();
  if (!me) return null;
  return <LiveProvider role={me.role}>{me.role === "provider" ? <ProviderApp /> : <CustomerApp />}</LiveProvider>;
}
