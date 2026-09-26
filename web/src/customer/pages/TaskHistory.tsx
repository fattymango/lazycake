import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StateBadge } from "@shared/components/StateBadge";
import { IconHistory } from "@shared/components/Icon";
import { dateTime } from "@shared/format";
import { useSSE } from "@shared/hooks/useSSE";
import type { PortalEvent, Task } from "@shared/types";

export function TaskHistory() {
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();

  async function load() {
    setError(null);
    try {
      setTasks(await apiGet<Task[]>("/api/portal/customer/tasks"));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load tasks");
    }
  }

  useEffect(() => {
    load();
  }, []);

  // A new task, or a state change on an existing one, must show up here
  // live - reload rather than patch in place, since a brand new task
  // isn't in `tasks` yet for an in-place patch to find.
  useSSE("/api/portal/customer/events", (data) => {
    if ((data as PortalEvent).type === "task_state") load();
  });

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold">Tasks</h1>
        <p className="text-sm text-muted mt-0.5">Every task you've submitted.</p>
      </div>
      <Panel>
        {tasks === null && !error ? (
          <LoadingState />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : (
          <DataTable
            rows={tasks!}
            rowKey={(t) => t.id}
            onRowClick={(t) => navigate(`/tasks/${t.id}`)}
            emptyLabel="No tasks yet."
            emptyIcon={IconHistory}
            columns={[
              { header: "Task", render: (t) => <span className="font-mono text-xs">{t.id}</span> },
              { header: "State", render: (t) => <StateBadge state={t.state} /> },
              { header: "Image", render: (t) => <span className="font-mono text-xs text-muted">{t.image}</span> },
              { header: "Node", render: (t) => <span className="font-mono text-xs text-muted">{t.node_id ?? "—"}</span> },
              { header: "Submitted", render: (t) => <span className="text-muted">{dateTime(t.created_at_ms)}</span> },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
