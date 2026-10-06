import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { apiGet, apiPost, ApiError, errorMessage, setUnauthorizedHandler } from "./api";
import type { ReturnState } from "./returnPath";
import type { Role, Whoami } from "./types";

interface AuthState {
  me: Whoami | null;
  /** First whoami check still in flight. */
  loading: boolean;
  /** The server could not be reached or errored on the first whoami check. */
  serverError: string | null;
  /** How the last session ended: the user signed out, or it expired/was revoked. Null while signed in. */
  endedBy: "logout" | "expired" | null;
  /** The role of the session that ended, so a return path isn't reused by a different role. */
  lastRole: Role | null;
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
  const [endedBy, setEndedBy] = useState<"logout" | "expired" | null>(null);
  const lastRoleRef = useRef<Role | null>(null);

  const refresh = useCallback(async () => {
    try {
      const who = await apiGet<Whoami>("/api/portal/me");
      lastRoleRef.current = who.role;
      setMe(who);
      setEndedBy(null);
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
    setUnauthorizedHandler(() => {
      setMe(null);
      setEndedBy((e) => e ?? "expired");
    });
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
      setEndedBy("logout");
      setMe(null);
    }
  }, []);

  const retry = useCallback(async () => {
    setLoading(true);
    await refresh();
  }, [refresh]);

  const value = useMemo(
    () => ({ me, loading, serverError, endedBy, lastRole: lastRoleRef.current, login, signup, logout, retry }),
    [me, loading, serverError, endedBy, login, signup, logout, retry]
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}

/**
 * Sends signed-out visitors to /login. It remembers where they were headed so
 * they can come back after signing in, except after an explicit logout: signing
 * out ends that visit, so the next sign-in starts at the overview (and possibly
 * as a different person or role).
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { me, endedBy, lastRole } = useAuth();
  const location = useLocation();
  if (!me) {
    const state: ReturnState | undefined =
      endedBy === "logout" ? undefined : { from: location.pathname + location.search, role: lastRole ?? undefined };
    return <Navigate to="/login" state={state} replace />;
  }
  return <>{children}</>;
}
