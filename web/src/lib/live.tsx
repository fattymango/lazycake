import { createContext, useCallback, useContext, useEffect, useMemo, useRef } from "react";
import type { ReactNode } from "react";
import { useEventStream, type StreamStatus } from "./hooks/useEventStream";
import type { PortalEvent, Role } from "./types";

type Handler = (event: PortalEvent) => void;

interface LiveState {
  status: StreamStatus;
  subscribe: (h: Handler) => () => void;
}

const LiveContext = createContext<LiveState | null>(null);

/**
 * Owns the single account event stream for the signed-in session and fans
 * events out to whichever components subscribed. Browsers cap concurrent
 * connections per host (6 over HTTP/1.1), so pages must not each open their
 * own EventSource: they call useLiveEvents() and share this one.
 */
export function LiveProvider({ role, children }: { role: Role; children: ReactNode }) {
  const handlers = useRef(new Set<Handler>());

  const status = useEventStream(`/api/portal/${role}/events`, (data) => {
    handlers.current.forEach((h) => h(data as PortalEvent));
  });

  const subscribe = useCallback((h: Handler) => {
    handlers.current.add(h);
    return () => {
      handlers.current.delete(h);
    };
  }, []);

  const value = useMemo(() => ({ status, subscribe }), [status, subscribe]);
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export function useLiveStatus(): StreamStatus {
  return useContext(LiveContext)?.status ?? "connecting";
}

/** Calls `handler` for every account event while the component is mounted. */
export function useLiveEvents(handler: Handler) {
  const ctx = useContext(LiveContext);
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => {
    if (!ctx) return;
    return ctx.subscribe((e) => ref.current(e));
  }, [ctx]);
}
