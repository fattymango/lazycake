import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { apiGet, apiPost, ApiError, errorMessage, setUnauthorizedHandler } from "./api";
import type { Role, Whoami } from "./types";

interface AuthState {
  me: Whoami | null;
  /** First whoami check still in flight. */
  loading: boolean;
  /** The server could not be reached or errored on the first whoami check. */
  serverError: string | null;
  login: (username: string, password: string) => Promise<void>;
  signup: (role: Role, username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  retry: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

// One app, one router, one session - which route tree mounts depends on the
// signed-in account's own role (me.role from GET /api/portal/me).
export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Whoami | null>(null);
  const [loading, setLoading] = useState(true);
  const [serverError, setServerError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setMe(await apiGet<Whoami>("/api/portal/me"));
      setServerError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setMe(null);
        setServerError(null);
      } else {
        // Not "signed out": we couldn't find out. Don't bounce to /login.
        setServerError(errorMessage(err, "The server returned an unexpected error."));
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // A 401 from any API call after sign-in means the session is gone.
  useEffect(() => {
    setUnauthorizedHandler(() => setMe(null));
    return () => setUnauthorizedHandler(null);
  }, []);

  const login = useCallback(
    async (username: string, password: string) => {
      await apiPost("/api/portal/login", { username, password });
      await refresh();
    },
    [refresh]
  );

  const signup = useCallback(
    async (role: Role, username: string, password: string) => {
      await apiPost(`/api/portal/${role}/signup`, { username, password });
      await refresh();
    },
    [refresh]
  );

  const logout = useCallback(async () => {
    try {
      await apiPost("/api/portal/logout");
    } finally {
      setMe(null);
    }
  }, []);

  const retry = useCallback(async () => {
    setLoading(true);
    await refresh();
  }, [refresh]);

  const value = useMemo(
    () => ({ me, loading, serverError, login, signup, logout, retry }),
    [me, loading, serverError, login, signup, logout, retry]
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}

/** Sends signed-out visitors to /login, remembering where they were headed. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { me } = useAuth();
  const location = useLocation();
  if (!me) return <Navigate to="/login" state={{ from: location.pathname + location.search }} replace />;
  return <>{children}</>;
}
