import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StateBadge } from "@shared/components/StateBadge";
import { Button } from "@shared/components/Form";
import { IconBilling, IconHistory, IconInbox, IconPlus } from "@shared/components/Icon";
import { money, relativeTime } from "@shared/format";
import { useRoleMe } from "@shared/hooks/useRoleMe";
import type { Me, Task } from "@shared/types";

export function Dashboard() {
  const { data: me, refresh } = useRoleMe<Me>("/api/portal/customer/me");
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
        <div>
          <h1 className="text-xl font-semibold">Dashboard</h1>
          <p className="text-sm text-muted mt-0.5">A quick look at your account and recent activity.</p>
        </div>
        <Button onClick={() => navigate("/tasks/new")}>
          <IconPlus className="w-4 h-4" /> Submit a task
        </Button>
      </div>

      <StatRow>
        <StatTile icon={IconBilling} label="Available balance" value={money(me?.available_balance_micros)} />
        <StatTile icon={IconBilling} label="Account balance" value={money(me?.balance_micros)} />
      </StatRow>

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
            emptyLabel="No tasks yet. Submit one to see it run here."
            emptyIcon={IconInbox}
            emptyAction={
              <Button onClick={() => navigate("/tasks/new")} size="sm">
                <IconPlus className="w-3.5 h-3.5" /> Submit a task
              </Button>
            }
            columns={[
              { header: "Task", render: (t) => <span className="font-mono text-xs">{t.id}</span> },
              { header: "State", render: (t) => <StateBadge state={t.state} /> },
              { header: "Image", render: (t) => <span className="font-mono text-xs text-muted">{t.image}</span> },
              { header: "Submitted", render: (t) => <span className="text-muted">{relativeTime(t.created_at_ms)}</span> },
            ]}
          />
        )}
      </Panel>

      {tasks && tasks.length > 0 && (
        <button
          onClick={() => navigate("/tasks")}
          className="self-start text-sm text-accent hover:underline flex items-center gap-1.5"
        >
          <IconHistory className="w-4 h-4" /> View all tasks
        </button>
      )}
    </div>
  );
}
