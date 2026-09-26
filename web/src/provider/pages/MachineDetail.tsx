import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { apiDelete, apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { ErrorState, LoadingState } from "@shared/components/States";
import { ConnectedBadge, StateBadge } from "@shared/components/StateBadge";
import { DataTable } from "@shared/components/DataTable";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { Button, FormError } from "@shared/components/Form";
import { ConfirmDialog } from "@shared/components/ConfirmDialog";
import { IconHistory, IconStop, IconTrash } from "@shared/components/Icon";
import { dateTime, relativeTime } from "@shared/format";
import { useSSE } from "@shared/hooks/useSSE";
import type { Node, PortalEvent, Task } from "@shared/types";

export function MachineDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

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

  // This machine connecting/disconnecting, or a task running on it
  // changing state, must update live - reload rather than patch fields in
  // place, since a task_state event doesn't say whether it's even for a
  // task on *this* node (the account-scoped stream carries every task the
  // account owns, not just this node's).
  useSSE("/api/portal/provider/events", (data) => {
    const ev = data as PortalEvent;
    if (ev.type === "node_connected" || ev.type === "node_disconnected" || ev.type === "task_state") {
      load();
    }
  });

  async function handleDelete() {
    if (!id) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await apiDelete(`/api/portal/provider/nodes/${id}`);
      navigate("/", { replace: true });
    } catch (err) {
      setDeleteError(err instanceof ApiError ? err.message : "failed to remove machine");
      setDeleting(false);
      setConfirmOpen(false);
    }
  }

  if (error) return <ErrorState message={error} onRetry={load} />;
  if (!node) return <LoadingState />;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Link to="/" className="text-xs text-muted hover:text-accent">
          ← Back to dashboard
        </Link>
        <div className="flex items-center justify-between mt-2">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-xl font-semibold">{node.hostname || node.id}</h1>
              <ConnectedBadge connected={node.connected} />
            </div>
            <div className="font-mono text-xs text-muted mt-1">{node.id}</div>
          </div>
          <Button variant="danger" onClick={() => setConfirmOpen(true)}>
            {node.connected ? (
              <>
                <IconStop className="w-4 h-4" /> Stop &amp; delete
              </>
            ) : (
              <>
                <IconTrash className="w-4 h-4" /> Delete
              </>
            )}
          </Button>
        </div>
        <FormError message={deleteError} />
      </div>

      <Panel title="Capacity & trust">
        <StatRow>
          <StatTile label="CPU cores" value={String(node.offer_cores)} />
          <StatTile label="Memory" value={`${node.offer_memory_mb} MB`} />
          <StatTile label="Disk" value={`${node.offer_disk_mb} MB`} />
          <StatTile label="Trust score" value={node.trust_score.toFixed(2)} />
          <StatTile label="Last heartbeat" value={relativeTime(node.last_heartbeat_at_ms)} />
        </StatRow>
      </Panel>

      <Panel title="Recent tasks">
        <DataTable
          rows={tasks}
          rowKey={(t) => t.id}
          emptyLabel="No tasks have run on this machine yet."
          emptyIcon={IconHistory}
          columns={[
            { header: "Task", render: (t) => <span className="font-mono text-xs">{t.id}</span> },
            { header: "State", render: (t) => <StateBadge state={t.state} /> },
            { header: "Image", render: (t) => <span className="font-mono text-xs text-muted">{t.image}</span> },
            { header: "Ran", render: (t) => <span className="text-muted">{dateTime(t.created_at_ms)}</span> },
          ]}
        />
      </Panel>

      <ConfirmDialog
        open={confirmOpen}
        title={node.connected ? "Stop and delete this machine?" : "Delete this machine?"}
        description={
          node.connected
            ? "This asks the running agent to shut down cleanly, then removes it from your fleet. If the agent doesn't respond (offline, unreachable), the machine may reappear until it's stopped some other way."
            : "This removes it from your fleet. Its task history is kept."
        }
        confirmLabel={node.connected ? "Stop & delete" : "Delete"}
        busy={deleting}
        onConfirm={handleDelete}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
}
