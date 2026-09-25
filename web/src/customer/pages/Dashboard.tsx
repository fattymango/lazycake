import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StateBadge } from "@shared/components/StateBadge";
import { Button } from "@shared/components/Form";
import { money, relativeTime } from "@shared/format";
import { useCustomerAuth } from "@shared/auth";
import type { Task } from "@shared/types";

export function Dashboard() {
  const { me, refresh } = useCustomerAuth();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();

  async function load() {
    setError(null);
    try {
      const t = await apiGet<Task[]>("/api/portal/customer/tasks?limit=10");
      setTasks(t);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load");
    }
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Dashboard</h1>
        <Button onClick={() => navigate("/tasks/new")}>Submit a task</Button>
      </div>

      <Panel title="Balance">
        <StatRow>
          <StatTile label="available balance" value={money(me?.available_balance_micros)} />
          <StatTile label="account balance" value={money(me?.balance_micros)} />
        </StatRow>
      </Panel>

      <Panel title="Recent tasks">
        {tasks === null && !error ? (
          <LoadingState />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : (
          <DataTable
            rows={tasks!}
            rowKey={(t) => t.id}
            onRowClick={(t) => navigate(`/tasks/${t.id}`)}
            emptyLabel="no tasks yet — submit one to get started"
            columns={[
              { header: "task", render: (t) => <span className="font-mono text-xs">{t.id}</span> },
              { header: "state", render: (t) => <StateBadge state={t.state} /> },
              { header: "image", render: (t) => <span className="font-mono text-xs text-muted">{t.image}</span> },
              { header: "submitted", render: (t) => relativeTime(t.created_at_ms) },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
