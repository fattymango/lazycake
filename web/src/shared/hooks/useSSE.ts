import { useEffect, useRef, useState } from "react";

// Subscribes to an account-scoped SSE stream (GET /api/portal/{role}/events)
// exactly the way internal/coordinator/dashboard/index.html's `connect()`
// does for the fleet-wide /events stream, minus the polling fallback - the
// browser's EventSource already reconnects on its own.
export function useSSE(url: string | null, onMessage: (data: unknown) => void) {
  const [live, setLive] = useState(false);
  const onMessageRef = useRef(onMessage);
  onMessageRef.current = onMessage;

  useEffect(() => {
    if (!url) return;
    const es = new EventSource(url, { withCredentials: true });

    es.onopen = () => setLive(true);
    es.onerror = () => setLive(false);
    es.onmessage = (msg) => {
      try {
        onMessageRef.current(JSON.parse(msg.data));
      } catch {
        // ignore malformed events
      }
    };

    return () => es.close();
  }, [url]);

  return live;
}
