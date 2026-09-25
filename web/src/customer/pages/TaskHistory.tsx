import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StateBadge } from "@shared/components/StateBadge";
import { dateTime } from "@shared/format";
import type { Task } from "@shared/types";

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

  return (
    <div>
      <h1 className="text-xl font-semibold mb-6">Tasks</h1>
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
            emptyLabel="no tasks yet"
            columns={[
              { header: "task", render: (t) => <span className="font-mono text-xs">{t.id}</span> },
              { header: "state", render: (t) => <StateBadge state={t.state} /> },
              { header: "image", render: (t) => <span className="font-mono text-xs text-muted">{t.image}</span> },
              { header: "node", render: (t) => <span className="font-mono text-xs text-muted">{t.node_id ?? "—"}</span> },
              { header: "submitted", render: (t) => dateTime(t.created_at_ms) },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
