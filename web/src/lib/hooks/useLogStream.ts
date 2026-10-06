import { useCallback, useEffect, useRef, useState } from "react";
import type { LogLine } from "../types";
import { closeOnPageHide, usePageRestore } from "./usePageRestore";

export type LogStatus = "connecting" | "live" | "ended" | "error";

/** Lines kept in memory; older ones are dropped (and reported as truncated). */
export const MAX_LOG_LINES = 5000;

/**
 * Streams a task's logs over SSE.
 *
 * Three behaviours here exist because of a real bug (a failed task's log
 * printed itself again every few seconds): the server ends the stream with an
 * explicit `end` event and this closes the EventSource on it (EventSource
 * would otherwise treat the close as a dropped connection and reconnect);
 * lines are de-duplicated by `seq`, so a resumed or replayed stream can never
 * show a line twice; and a stream that ends is never reopened until the
 * caller asks via `restart`.
 */
export function useLogStream(taskId: string | undefined) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [status, setStatus] = useState<LogStatus>("connecting");
  const [truncated, setTruncated] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const restored = usePageRestore();

  const lastSeq = useRef(0);
  const pending = useRef<LogLine[]>([]);
  const frame = useRef<number | null>(null);

  const flush = useCallback(() => {
    frame.current = null;
    const batch = pending.current;
    if (batch.length === 0) return;
    pending.current = [];
    setLines((cur) => {
      const next = cur.length + batch.length > MAX_LOG_LINES ? [...cur, ...batch].slice(-MAX_LOG_LINES) : [...cur, ...batch];
      if (cur.length + batch.length > MAX_LOG_LINES) setTruncated(true);
      return next;
    });
  }, []);

  useEffect(() => {
    if (!taskId) return;
    setLines([]);
    setTruncated(false);
    setStatus("connecting");
    lastSeq.current = 0;
    pending.current = [];

    const es = new EventSource(`/api/portal/customer/tasks/${encodeURIComponent(taskId)}/logs`, { withCredentials: true });

    es.onopen = () => setStatus((s) => (s === "ended" ? s : "live"));
    es.onmessage = (msg) => {
      try {
        const line: LogLine = JSON.parse(msg.data);
        if (line.seq <= lastSeq.current) return; // already have it
        lastSeq.current = line.seq;
        pending.current.push(line);
        // Coalesce a burst of lines into one render.
        if (frame.current === null) frame.current = requestAnimationFrame(flush);
      } catch {
        // ignore a malformed frame
      }
    };
    es.addEventListener("end", () => {
      flush();
      setStatus("ended");
      es.close();
    });
    es.onerror = () => {
      // CLOSED means the browser gave up (e.g. a 4xx); otherwise it is
      // reconnecting by itself and Last-Event-ID resumes where we were.
      setStatus(es.readyState === EventSource.CLOSED ? "error" : "connecting");
    };

    const stopListening = closeOnPageHide(es);
    return () => {
      stopListening();
      es.close();
      if (frame.current !== null) cancelAnimationFrame(frame.current);
      frame.current = null;
    };
  }, [taskId, attempt, restored, flush]);

  const restart = useCallback(() => setAttempt((a) => a + 1), []);
  return { lines, status, truncated, restart };
}
