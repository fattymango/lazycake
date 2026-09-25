import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { apiGet, apiPost, ApiError, portalPath } from "./api";
import type { Me, ProviderMe, Role } from "./types";

interface AuthState<T> {
  me: T | null;
  loading: boolean;
  error: string | null;
  login: (username: string, password: string) => Promise<void>;
  signup: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

function makeAuthContext<T extends { account_id: string }>() {
  return createContext<AuthState<T> | null>(null);
}

const CustomerAuthContext = makeAuthContext<Me>();
const ProviderAuthContext = makeAuthContext<ProviderMe>();

function useProvideAuth<T extends { account_id: string }>(role: Role): AuthState<T> {
  const [me, setMe] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const m = await apiGet<T>(portalPath(role, "/me"));
      setMe(m);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setMe(null);
      } else {
        throw err;
      }
    } finally {
      setLoading(false);
    }
  }, [role]);

  useEffect(() => {
    refresh().catch(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const login = useCallback(
    async (username: string, password: string) => {
      setError(null);
      try {
        await apiPost(portalPath(role, "/login"), { username, password });
        await refresh();
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "login failed");
        throw err;
      }
    },
    [role, refresh]
  );

  const signup = useCallback(
    async (username: string, password: string) => {
      setError(null);
      try {
        await apiPost(portalPath(role, "/signup"), { username, password });
        await refresh();
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "signup failed");
        throw err;
      }
    },
    [role, refresh]
  );

  const logout = useCallback(async () => {
    await apiPost("/api/portal/logout");
    setMe(null);
  }, []);

  return useMemo(() => ({ me, loading, error, login, signup, logout, refresh }), [me, loading, error, login, signup, logout, refresh]);
}

export function CustomerAuthProvider({ children }: { children: ReactNode }) {
  const value = useProvideAuth<Me>("customer");
  return <CustomerAuthContext.Provider value={value}>{children}</CustomerAuthContext.Provider>;
}

export function ProviderAuthProvider({ children }: { children: ReactNode }) {
  const value = useProvideAuth<ProviderMe>("provider");
  return <ProviderAuthContext.Provider value={value}>{children}</ProviderAuthContext.Provider>;
}

export function useCustomerAuth(): AuthState<Me> {
  const ctx = useContext(CustomerAuthContext);
  if (!ctx) throw new Error("useCustomerAuth must be used within CustomerAuthProvider");
  return ctx;
}

export function useProviderAuth(): AuthState<ProviderMe> {
  const ctx = useContext(ProviderAuthContext);
  if (!ctx) throw new Error("useProviderAuth must be used within ProviderAuthProvider");
  return ctx;
}

// RequireAuth gates a route behind a signed-in session, redirecting to
// /login and remembering where the user was headed (task 7.7's "protected
// route redirects to login").
export function RequireAuth({
  loading,
  authed,
  children,
}: {
  loading: boolean;
  authed: boolean;
  children: ReactNode;
}) {
  const location = useLocation();
  if (loading) return <FullPageSpinner />;
  if (!authed) return <Navigate to="/login" state={{ from: location }} replace />;
  return <>{children}</>;
}

export function FullPageSpinner() {
  return (
    <div className="min-h-screen flex items-center justify-center text-muted text-sm">
      loading…
    </div>
  );
}
