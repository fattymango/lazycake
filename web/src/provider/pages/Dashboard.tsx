import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiDelete, apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { ConnectedBadge } from "@shared/components/StateBadge";
import { Button } from "@shared/components/Form";
import { ConfirmDialog } from "@shared/components/ConfirmDialog";
import { IconEarnings, IconMachine, IconPlus, IconStop, IconTrash } from "@shared/components/Icon";
import { money, relativeTime } from "@shared/format";
import { useRoleMe } from "@shared/hooks/useRoleMe";
import type { Node, ProviderMe } from "@shared/types";

export function Dashboard() {
  const { data: me, refresh } = useRoleMe<ProviderMe>("/api/portal/provider/me");
  const [nodes, setNodes] = useState<Node[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<Node | null>(null);
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();

  async function load() {
    setError(null);
    try {
      setNodes(await apiGet<Node[]>("/api/portal/provider/nodes"));
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load");
    }
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleDelete() {
    if (!pendingDelete) return;
    setDeleting(true);
    try {
      await apiDelete(`/api/portal/provider/nodes/${pendingDelete.id}`);
      setPendingDelete(null);
      await load();
    } finally {
      setDeleting(false);
    }
  }

  const connectedCount = nodes?.filter((n) => n.connected).length ?? 0;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Dashboard</h1>
          <p className="text-sm text-muted mt-0.5">Your fleet, at a glance.</p>
        </div>
        <Button onClick={() => navigate("/machines/add")}>
          <IconPlus className="w-4 h-4" /> Add a machine
        </Button>
      </div>

      <StatRow>
        <StatTile icon={IconMachine} label="Connected machines" value={`${connectedCount} / ${nodes?.length ?? 0}`} />
        <StatTile icon={IconEarnings} label="Lifetime earnings" value={money(me?.lifetime_earnings_micros)} tone="good" />
      </StatRow>

      <Panel title="Machines">
        {nodes === null && !error ? (
          <LoadingState />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : (
          <DataTable
            rows={nodes!}
            rowKey={(n) => n.id}
            onRowClick={(n) => navigate(`/machines/${n.id}`)}
            emptyLabel="No machines yet. Add one to start earning."
            emptyIcon={IconMachine}
            emptyAction={
              <Button onClick={() => navigate("/machines/add")} size="sm">
                <IconPlus className="w-3.5 h-3.5" /> Add a machine
              </Button>
            }
            columns={[
              { header: "Hostname", render: (n) => n.hostname || n.id },
              { header: "ID", render: (n) => <span className="font-mono text-xs text-muted">{n.id}</span> },
              { header: "Status", render: (n) => <ConnectedBadge connected={n.connected} /> },
              { header: "Trust", render: (n) => n.trust_score.toFixed(2) },
              { header: "Last seen", render: (n) => <span className="text-muted">{relativeTime(n.last_heartbeat_at_ms)}</span> },
              {
                header: "",
                className: "text-right",
                render: (n) => (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={(e) => {
                      e.stopPropagation();
                      setPendingDelete(n);
                    }}
                  >
                    {n.connected ? <IconStop className="w-4 h-4" /> : <IconTrash className="w-4 h-4" />}
                  </Button>
                ),
              },
            ]}
          />
        )}
      </Panel>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={pendingDelete?.connected ? "Stop and delete this machine?" : "Delete this machine?"}
        description={
          pendingDelete?.connected
            ? `This asks ${pendingDelete.hostname || pendingDelete.id} to shut down cleanly, then removes it from your fleet.`
            : `This removes ${pendingDelete?.hostname || pendingDelete?.id} from your fleet. Its task history is kept.`
        }
        confirmLabel={pendingDelete?.connected ? "Stop & delete" : "Delete"}
        busy={deleting}
        onConfirm={handleDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
