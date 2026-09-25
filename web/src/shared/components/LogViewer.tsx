import { useEffect, useRef } from "react";
import type { LogLine } from "../types";

// Auto-scrolling log pane for task detail's live logs (task 7.8), styled
// like a terminal - stderr lines tinted so failures are easy to spot in a
// long run.
export function LogViewer({ lines }: { lines: LogLine[] }) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [lines.length]);

  return (
    <div className="bg-black/40 border border-border rounded font-mono text-xs p-3 h-96 overflow-y-auto whitespace-pre-wrap break-all">
      {lines.length === 0 ? (
        <div className="text-muted">no output yet</div>
      ) : (
        lines.map((l) => (
          <div key={l.seq} className={l.stream === "stderr" ? "text-bad" : "text-text"}>
            {l.line}
          </div>
        ))
      )}
      <div ref={bottomRef} />
    </div>
  );
}
