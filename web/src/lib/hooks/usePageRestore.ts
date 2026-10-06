import { useEffect, useState } from "react";

/**
 * Returns a number that increases each time the page is restored from the
 * browser's back/forward cache. Streaming hooks list it as a dependency so
 * they reconnect after a restore.
 *
 * Why this exists: when you leave a page by a full navigation, the browser
 * may park the old document in its back/forward cache instead of destroying
 * it, and an open EventSource stays open inside it. Those parked streams add
 * up against the browser's per-host connection limit (6 over HTTP/1.1) until
 * new requests stall. Hooks therefore close their stream on `pagehide` (see
 * closeOnPageHide) and reconnect when this value changes.
 */
export function usePageRestore(): number {
  const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    const onShow = (e: PageTransitionEvent) => {
      if (e.persisted) setEpoch((n) => n + 1);
    };
    window.addEventListener("pageshow", onShow);
    return () => window.removeEventListener("pageshow", onShow);
  }, []);
  return epoch;
}

/** Closes `es` when the page is hidden/unloaded; returns a cleanup that removes the listener. */
export function closeOnPageHide(es: EventSource): () => void {
  const close = () => es.close();
  window.addEventListener("pagehide", close);
  return () => window.removeEventListener("pagehide", close);
}
