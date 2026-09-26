import { useCallback, useEffect, useState } from "react";
import { apiGet, ApiError } from "../api";

// The role-specific "extra" data (balance for a customer, lifetime
// earnings for a provider) that useAuth()'s role-agnostic Whoami
// deliberately doesn't carry - fetched separately by whichever page or
// shell actually needs it, against the existing per-role /me endpoints.
export function useRoleMe<T>(path: string) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setData(await apiGet<T>(path));
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load");
    }
  }, [path]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { data, error, refresh };
}
