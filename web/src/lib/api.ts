import type { Role } from "./types";

// Thin fetch wrapper for /api/portal/*. Auth is the httpOnly session cookie
// the login/signup endpoints set, so every call sends credentials. Every
// non-2xx is expected to carry an {"error": "..."} JSON body.
export class ApiError extends Error {
  status: number;
  /** True when the request never reached the server (offline, DNS, refused). */
  network: boolean;
  constructor(status: number, message: string, network = false) {
    super(message);
    this.status = status;
    this.network = network;
  }
}

type UnauthorizedHandler = () => void;
let onUnauthorized: UnauthorizedHandler | null = null;

/**
 * Registers what to do when any call comes back 401 on a session that was
 * believed to be live (expired, revoked, signed in elsewhere). The auth
 * provider uses it to drop the session so the router sends the user to the
 * sign-in page instead of leaving every widget showing its own error.
 */
export function setUnauthorizedHandler(h: UnauthorizedHandler | null) {
  onUnauthorized = h;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      ...init,
      credentials: "include",
      headers: {
        ...(init?.body ? { "Content-Type": "application/json" } : {}),
        ...init?.headers,
      },
    });
  } catch (err) {
    if ((err as Error)?.name === "AbortError") throw err;
    throw new ApiError(0, "Can't reach the LazyCake server. Check your connection and try again.", true);
  }

  if (!res.ok) {
    let message = res.statusText || `Request failed (${res.status})`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // non-JSON error body; keep statusText
    }
    // /me and the login endpoints answer 401 as part of normal flow; only a
    // 401 from anywhere else means the session died.
    if (res.status === 401 && !path.startsWith("/api/portal/me") && !path.includes("/login") && !path.includes("/signup")) {
      onUnauthorized?.();
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  return request<T>(path, { method: "GET", signal });
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, { method: "POST", body: body !== undefined ? JSON.stringify(body) : undefined });
}

export function apiDelete<T>(path: string): Promise<T> {
  return request<T>(path, { method: "DELETE" });
}

export function portalPath(role: Role, suffix: string): string {
  return `/api/portal/${role}${suffix}`;
}

/** A message safe to show a person, whatever was thrown. */
export function errorMessage(err: unknown, fallback = "Something went wrong"): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}
