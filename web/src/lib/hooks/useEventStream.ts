import { useEffect, useRef, useState } from "react";

export type StreamStatus = "connecting" | "live" | "offline";

/**
 * Subscribes to an account-scoped SSE stream (GET /api/portal/{role}/events).
 * EventSource reconnects by itself after a drop, so "offline" here means
 * "currently between connections", not "gave up".
 */
export function useEventStream(url: string | null, onMessage: (data: unknown) => void): StreamStatus {
  const [status, setStatus] = useState<StreamStatus>("connecting");
  const onMessageRef = useRef(onMessage);
  onMessageRef.current = onMessage;

  useEffect(() => {
    if (!url) return;
    setStatus("connecting");
    const es = new EventSource(url, { withCredentials: true });
    es.onopen = () => setStatus("live");
    es.onerror = () => setStatus("offline");
    es.onmessage = (msg) => {
      try {
        onMessageRef.current(JSON.parse(msg.data));
      } catch {
        // ignore a malformed frame rather than tearing the stream down
      }
    };
    return () => es.close();
  }, [url]);

  return status;
}
