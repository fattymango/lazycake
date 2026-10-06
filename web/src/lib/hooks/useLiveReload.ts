import { useEffect, useRef } from "react";
import { useLiveEvents } from "../live";
import type { PortalEvent } from "../types";

/**
 * Calls `reload` shortly after a matching live event. A burst of events (a
 * fleet changing state at once) coalesces into one reload rather than a
 * request per event.
 */
export function useLiveReload(reload: () => void, match: (e: PortalEvent) => boolean = () => true, delayMs = 400) {
  const timer = useRef<number>();
  const reloadRef = useRef(reload);
  reloadRef.current = reload;
  const matchRef = useRef(match);
  matchRef.current = match;

  useLiveEvents((e) => {
    if (!matchRef.current(e)) return;
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => reloadRef.current(), delayMs);
  });
  useEffect(() => () => window.clearTimeout(timer.current), []);
}
