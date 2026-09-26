import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { apiGet, apiPost, ApiError } from "./api";
import type { Role, Whoami } from "./types";

interface AuthState {
  me: Whoami | null;
  loading: boolean;
  error: string | null;
  // login/signup never take a role: an account's role is fixed at signup
  // and the server already knows it from the username alone (POST
  // /api/portal/login) - that's the entire point of this rewrite, see the
  // module doc comment below. signup is the one place a role is ever
  // chosen, since it doesn't exist to look up yet.
  login: (username: string, password: string) => Promise<void>;
  signup: (role: Role, username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

// One auth context, one session, one login form - the account's role
// (customer or provider) comes from the server (GET /api/portal/me) once,
// not from which page you happened to load or a picker you have to answer
// every time. Replaces the earlier per-role CustomerAuthContext/
// ProviderAuthContext pair, which required knowing the role before you
// could even check whether you were signed in.
const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Whoami | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const m = await apiGet<Whoami>("/api/portal/me");
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
  }, []);

  useEffect(() => {
    refresh().catch(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const login = useCallback(
    async (username: string, password: string) => {
      setError(null);
      try {
        await apiPost("/api/portal/login", { username, password });
        await refresh();
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "login failed");
        throw err;
      }
    },
    [refresh]
  );

  const signup = useCallback(
    async (role: Role, username: string, password: string) => {
      setError(null);
      try {
        await apiPost(`/api/portal/${role}/signup`, { username, password });
        await refresh();
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "signup failed");
        throw err;
      }
    },
    [refresh]
  );

  const logout = useCallback(async () => {
    await apiPost("/api/portal/logout");
    setMe(null);
  }, []);

  const value = useMemo(() => ({ me, loading, error, login, signup, logout, refresh }), [me, loading, error, login, signup, logout, refresh]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}

// RequireAuth gates a route behind a signed-in session, redirecting to
// /login and remembering where the user was headed.
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
    <div className="min-h-screen flex items-center justify-center bg-bg">
      <div className="w-8 h-8 rounded-full border-2 border-border border-t-accent animate-spin" />
    </div>
  );
}
