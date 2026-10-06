import { useCallback, useEffect, useRef, useState } from "react";
import type { DependencyList } from "react";
import { ApiError, errorMessage } from "../api";

export interface AsyncState<T> {
  data: T | undefined;
  error: string | null;
  /** HTTP status of the failure, when there was one (404 -> "not found" screen). */
  errorStatus: number | null;
  /** First load, nothing to show yet. */
  loading: boolean;
  /** A reload is in flight but `data` from the previous load is still valid. */
  refreshing: boolean;
  reload: () => Promise<void>;
  setData: (updater: (prev: T | undefined) => T | undefined) => void;
}

/**
 * Runs `fn` on mount and whenever `deps` change; keeps showing the previous
 * data while a reload is in flight (so live updates don't flash a spinner),
 * and ignores responses that arrive after a newer request was started.
 */
export function useAsync<T>(fn: (signal: AbortSignal) => Promise<T>, deps: DependencyList): AsyncState<T> {
  const [data, setDataState] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [errorStatus, setErrorStatus] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const seq = useRef(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;

  const run = useCallback(async (initial: boolean) => {
    const mine = ++seq.current;
    const ctrl = new AbortController();
    if (initial) setLoading(true);
    else setRefreshing(true);
    try {
      const result = await fnRef.current(ctrl.signal);
      if (mine !== seq.current) return;
      setDataState(result);
      setError(null);
      setErrorStatus(null);
    } catch (err) {
      if (mine !== seq.current || (err as Error)?.name === "AbortError") return;
      setError(errorMessage(err, "Failed to load"));
      setErrorStatus(err instanceof ApiError ? err.status : null);
    } finally {
      if (mine === seq.current) {
        setLoading(false);
        setRefreshing(false);
      }
    }
  }, []);

  useEffect(() => {
    setDataState(undefined);
    setError(null);
    setErrorStatus(null);
    run(true);
    return () => {
      seq.current++; // invalidate in-flight work for the old deps
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  const reload = useCallback(() => run(false), [run]);
  const setData = useCallback((updater: (prev: T | undefined) => T | undefined) => setDataState(updater), []);
  return { data, error, errorStatus, loading, refreshing, reload, setData };
}
