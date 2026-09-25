import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { ErrorState, LoadingState } from "@shared/components/States";
import { ConnectedBadge, StateBadge } from "@shared/components/StateBadge";
import { DataTable } from "@shared/components/DataTable";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { dateTime, relativeTime } from "@shared/format";
import type { Node, Task } from "@shared/types";

export function MachineDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!id) return;
    setError(null);
    try {
      const [n, t] = await Promise.all([
        apiGet<Node>(`/api/portal/provider/nodes/${id}`),
        apiGet<Task[]>(`/api/portal/provider/nodes/${id}/tasks`),
      ]);
      setNode(n);
      setTasks(t);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load machine");
    }
  }, [id]);

  useEffect(() => {
    load();
  }, [load]);

  if (error) return <ErrorState message={error} onRetry={load} />;
  if (!node) return <LoadingState />;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Link to="/" className="text-xs text-muted hover:text-accent">
          ← back to dashboard
        </Link>
        <div className="flex items-center gap-3 mt-2">
          <h1 className="text-xl font-semibold">{node.hostname || node.id}</h1>
          <ConnectedBadge connected={node.connected} />
        </div>
        <div className="font-mono text-xs text-muted mt-1">{node.id}</div>
      </div>

      <Panel title="Capacity & trust">
        <StatRow>
          <StatTile label="cpu cores" value={String(node.offer_cores)} />
          <StatTile label="memory" value={`${node.offer_memory_mb} MB`} />
          <StatTile label="disk" value={`${node.offer_disk_mb} MB`} />
          <StatTile label="trust score" value={node.trust_score.toFixed(2)} />
          <StatTile label="last heartbeat" value={relativeTime(node.last_heartbeat_at_ms)} />
        </StatRow>
      </Panel>

      <Panel title="Recent tasks">
        <DataTable
          rows={tasks}
          rowKey={(t) => t.id}
          emptyLabel="no tasks have run on this machine yet"
          columns={[
            { header: "task", render: (t) => <span className="font-mono text-xs">{t.id}</span> },
            { header: "state", render: (t) => <StateBadge state={t.state} /> },
            { header: "image", render: (t) => <span className="font-mono text-xs text-muted">{t.image}</span> },
            { header: "ran", render: (t) => dateTime(t.created_at_ms) },
          ]}
        />
      </Panel>
    </div>
  );
}
