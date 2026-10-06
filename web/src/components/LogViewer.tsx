import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowDownToLine, Download, Search, WrapText, Clock, AlertCircle } from "lucide-react";
import { cn } from "@/lib/cn";
import { copyToClipboard } from "@/lib/clipboard";
import { clockTime } from "@/lib/format";
import { MAX_LOG_LINES, type LogStatus } from "@/lib/hooks/useLogStream";
import type { LogLine } from "@/lib/types";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Input } from "@/ui/Input";
import { Toggle } from "@/ui/Toggle";
import { useToast } from "@/ui/Toast";
import { Copy } from "lucide-react";

// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;?]*[A-Za-z]/g;
const clean = (s: string) => s.replace(ANSI, "");

function highlight(text: string, query: string) {
  if (!query) return text;
  const lower = text.toLowerCase();
  const q = query.toLowerCase();
  const out: React.ReactNode[] = [];
  let i = 0;
  for (let at = lower.indexOf(q); at !== -1; at = lower.indexOf(q, i)) {
    if (at > i) out.push(text.slice(i, at));
    out.push(
      <mark key={at} className="rounded-sm bg-warning/35 text-inherit">
        {text.slice(at, at + q.length)}
      </mark>
    );
    i = at + q.length;
  }
  out.push(text.slice(i));
  return out;
}

function StatusBadge({ status, count }: { status: LogStatus; count: number }) {
  if (status === "live")
    return (
      <Badge tone="success" dot pulse size="sm">
        Live
      </Badge>
    );
  if (status === "ended")
    return (
      <Badge tone="neutral" size="sm">
        Finished · {count} {count === 1 ? "line" : "lines"}
      </Badge>
    );
  if (status === "error")
    return (
      <Badge tone="danger" size="sm">
        Disconnected
      </Badge>
    );
  return (
    <Badge tone="neutral" dot pulse size="sm">
      Connecting
    </Badge>
  );
}

/**
 * Terminal-style output pane: line numbers, optional timestamps, stderr
 * tinting, search with highlighting, wrap, follow-the-tail (which turns
 * itself off when you scroll up), copy and download.
 */
export function LogViewer({
  taskId,
  lines,
  status,
  truncated,
  onRetry,
  heightClass = "h-[30rem]",
  taskFinished,
}: {
  taskId: string;
  lines: LogLine[];
  status: LogStatus;
  truncated: boolean;
  onRetry: () => void;
  heightClass?: string;
  /** The task is in a terminal state (so "no output" is final, not "yet"). */
  taskFinished: boolean;
}) {
  const toast = useToast();
  const scroller = useRef<HTMLDivElement>(null);
  const [wrap, setWrap] = useState(true);
  const [times, setTimes] = useState(false);
  const [follow, setFollow] = useState(true);
  const [query, setQuery] = useState("");
  const [errorsOnly, setErrorsOnly] = useState(false);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return lines.filter((l) => (!errorsOnly || l.stream === "stderr") && (!q || clean(l.line).toLowerCase().includes(q)));
  }, [lines, query, errorsOnly]);
  const errCount = useMemo(() => lines.reduce((n, l) => n + (l.stream === "stderr" ? 1 : 0), 0), [lines]);

  // Stick to the bottom while following.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && follow) el.scrollTop = el.scrollHeight;
  }, [shown.length, follow, wrap]);

  // Scrolling up turns follow off; scrolling back to the bottom turns it on.
  const onScroll = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    setFollow((f) => (f === atBottom ? f : atBottom));
  }, []);

  useEffect(() => {
    setFollow(true);
  }, [taskId]);

  const asText = useCallback(
    () => lines.map((l) => `${new Date(l.at_ms).toISOString()} ${l.stream === "stderr" ? "ERR" : "OUT"} ${clean(l.line)}`).join("\n"),
    [lines]
  );

  async function copyAll() {
    if (await copyToClipboard(lines.map((l) => clean(l.line)).join("\n"))) toast.success("Output copied", `${lines.length} lines`);
  }

  function download() {
    const url = URL.createObjectURL(new Blob([asText() + "\n"], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${taskId}.log`;
    a.click();
    URL.revokeObjectURL(url);
  }

  const empty = lines.length === 0;

  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-xl border border-border bg-term-bg shadow-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-term-border px-3 py-2">
        <StatusBadge status={status} count={lines.length} />
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          <Input
            leading={<Search />}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search output"
            aria-label="Search output"
            className="w-40 sm:w-48 [&_input]:h-7 [&_input]:border-term-border [&_input]:bg-transparent [&_input]:text-xs [&_input]:text-term-fg"
          />
          <Toggle
            pressed={errorsOnly}
            onClick={() => setErrorsOnly((v) => !v)}
            disabled={errCount === 0}
            title="Show only stderr lines"
            className="text-term-muted hover:bg-term-border hover:text-term-fg"
          >
            <AlertCircle />
            Errors{errCount > 0 && <span data-tnum>{errCount}</span>}
          </Toggle>
          <Toggle
            pressed={times}
            onClick={() => setTimes((v) => !v)}
            title="Show timestamps"
            className="text-term-muted hover:bg-term-border hover:text-term-fg"
          >
            <Clock />
            Time
          </Toggle>
          <Toggle
            pressed={wrap}
            onClick={() => setWrap((v) => !v)}
            title="Wrap long lines"
            className="text-term-muted hover:bg-term-border hover:text-term-fg"
          >
            <WrapText />
            Wrap
          </Toggle>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={copyAll}
            disabled={empty}
            aria-label="Copy output"
            className="text-term-muted hover:bg-term-border hover:text-term-fg"
          >
            <Copy />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={download}
            disabled={empty}
            aria-label="Download output"
            className="text-term-muted hover:bg-term-border hover:text-term-fg"
          >
            <Download />
          </Button>
        </div>
      </div>

      <div className="relative">
        <div
          ref={scroller}
          onScroll={onScroll}
          data-scroll-x
          role="log"
          aria-live="off"
          aria-label="Task output"
          tabIndex={0}
          className={cn("overflow-auto py-2 font-mono text-[0.78rem] leading-[1.35rem] text-term-fg", heightClass)}
        >
          {truncated && (
            <p className="px-3 pb-2 text-2xs text-term-muted">
              Showing the latest {MAX_LOG_LINES.toLocaleString()} lines. Download for the full output you've received.
            </p>
          )}
          {empty ? (
            <div className="flex h-full min-h-[8rem] items-center justify-center px-6 text-center text-term-muted">
              {status === "error" ? (
                <div>
                  <p>Couldn't load the output.</p>
                  <Button variant="secondary" size="sm" className="mt-3" onClick={onRetry}>
                    Try again
                  </Button>
                </div>
              ) : taskFinished || status === "ended" ? (
                <p>This task produced no output.</p>
              ) : (
                <p>Waiting for output…</p>
              )}
            </div>
          ) : shown.length === 0 ? (
            <p className="px-3 py-6 text-center text-term-muted">No lines match.</p>
          ) : (
            shown.map((l) => (
              <div key={l.seq} className={cn("flex gap-3 px-3 hover:bg-white/[0.03]", l.stream === "stderr" && "bg-danger/[0.08]")}>
                <span className="w-10 shrink-0 select-none text-right text-term-muted/70" aria-hidden data-tnum>
                  {l.seq}
                </span>
                {times && (
                  <span className="shrink-0 select-none text-term-muted" aria-hidden data-tnum>
                    {clockTime(l.at_ms)}
                  </span>
                )}
                <span
                  className={cn(
                    "min-w-0 flex-1",
                    wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre",
                    l.stream === "stderr" && "text-term-err"
                  )}
                >
                  {highlight(clean(l.line), query.trim())}
                </span>
              </div>
            ))
          )}
        </div>
        {!follow && !empty && (
          <Button
            size="sm"
            variant="secondary"
            className="absolute bottom-3 right-4 shadow-pop"
            onClick={() => {
              setFollow(true);
              const el = scroller.current;
              if (el) el.scrollTop = el.scrollHeight;
            }}
          >
            <ArrowDownToLine />
            Jump to latest
          </Button>
        )}
      </div>
    </div>
  );
}
