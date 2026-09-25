import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StateBadge } from "@shared/components/StateBadge";
import { LogViewer } from "@shared/components/LogViewer";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { dateTime, money } from "@shared/format";
import type { LogLine, PortalEvent, Task } from "@shared/types";

export function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const [task, setTask] = useState<Task | null>(null);
  const [logs, setLogs] = useState<LogLine[]>([]);
  const [error, setError] = useState<string | null>(null);

  const loadTask = useCallback(async () => {
    if (!id) return;
    try {
      setTask(await apiGet<Task>(`/api/portal/customer/tasks/${id}`));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load task");
    }
  }, [id]);

  useEffect(() => {
    loadTask();
  }, [loadTask]);

  // Live logs via SSE (task 7.4: GET .../tasks/{id}/logs, "same polling
  // shape StreamLogs already uses server-side") - re-render the task's own
  // state from the account-scoped event stream too, so a state change
  // (queued -> running -> succeeded) shows up without a page refresh.
  useEffect(() => {
    if (!id) return;
    const es = new EventSource(`/api/portal/customer/tasks/${id}/logs`, { withCredentials: true });
    es.onmessage = (msg) => {
      try {
        const line: LogLine = JSON.parse(msg.data);
        setLogs((cur) => [...cur, line]);
      } catch {
        // ignore malformed line
      }
    };
    return () => es.close();
  }, [id]);

  useEffect(() => {
    if (!id) return;
    const es = new EventSource("/api/portal/customer/events", { withCredentials: true });
    es.onmessage = (msg) => {
      try {
        const ev: PortalEvent = JSON.parse(msg.data);
        if (ev.type === "task_state" && ev.task_id === id) {
          setTask((cur) => (cur ? { ...cur, state: ev.state, node_id: ev.node_id ?? cur.node_id } : cur));
        }
      } catch {
        // ignore
      }
    };
    return () => es.close();
  }, [id]);

  if (error) return <ErrorState message={error} onRetry={loadTask} />;
  if (!task) return <LoadingState />;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Link to="/tasks" className="text-xs text-muted hover:text-accent">
          ← back to tasks
        </Link>
        <div className="flex items-center gap-3 mt-2">
          <h1 className="text-xl font-semibold font-mono">{task.id}</h1>
          <StateBadge state={task.state} />
        </div>
      </div>

      <Panel title="Details">
        <StatRow>
          <StatTile label="image" value={task.image} />
          <StatTile label="node" value={task.node_id ?? "unassigned"} />
          <StatTile label="cores" value={String(task.cores)} />
          <StatTile label="memory" value={`${task.memory_mb} MB`} />
        </StatRow>
        <div className="grid grid-cols-2 gap-4 mt-4 text-sm">
          <div>
            <div className="text-xs text-muted">submitted</div>
            <div>{dateTime(task.created_at_ms)}</div>
          </div>
          <div>
            <div className="text-xs text-muted">finished</div>
            <div>{task.finished_at_ms ? dateTime(task.finished_at_ms) : "—"}</div>
          </div>
          {task.exit_reason && (
            <div>
              <div className="text-xs text-muted">exit reason</div>
              <div className="text-bad">{task.exit_reason}</div>
            </div>
          )}
          {task.price_micros !== undefined && (
            <div>
              <div className="text-xs text-muted">price</div>
              <div>{money(task.price_micros)}</div>
            </div>
          )}
        </div>
      </Panel>

      <Panel title="Logs">
        <LogViewer lines={logs} />
      </Panel>
    </div>
  );
}
