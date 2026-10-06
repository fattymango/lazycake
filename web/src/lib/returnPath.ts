import type { Role } from "./types";

/** What the route guard remembers when it sends a signed-out visitor to /login. */
export interface ReturnState {
  from?: string;
  /** The role of the session that ended, if there was one. */
  role?: Role;
}

/**
 * Where to go after signing in. The page you were on is only honoured when it
 * is safe to assume it still makes sense for whoever is signing in:
 *  - an explicit logout never remembers a page (the guard passes no state);
 *  - a page remembered from an ended session is dropped if a *different role*
 *    signs in (a customer's /tasks/<id> doesn't exist for a provider);
 *  - a deep link opened while signed out (no previous role) is honoured.
 * It must also be a same-site path, so a crafted state can't redirect elsewhere.
 */
export function resolveReturnPath(state: ReturnState | null | undefined, signedInRole: Role): string {
  const from = state?.from;
  if (!from || !from.startsWith("/") || from.startsWith("//") || from.startsWith("/login") || from.startsWith("/signup")) return "/";
  if (state?.role && state.role !== signedInRole) return "/";
  return from;
}
