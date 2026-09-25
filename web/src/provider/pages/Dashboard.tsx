import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { ConnectedBadge } from "@shared/components/StateBadge";
import { Button } from "@shared/components/Form";
import { money, relativeTime } from "@shared/format";
import { useProviderAuth } from "@shared/auth";
import type { Node } from "@shared/types";

export function Dashboard() {
  const { me, refresh } = useProviderAuth();
  const [nodes, setNodes] = useState<Node[] | null>(null);
  const [error, setError] = useState<string | null>(null);
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

  const connectedCount = nodes?.filter((n) => n.connected).length ?? 0;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Dashboard</h1>
        <Button onClick={() => navigate("/machines/add")}>Add a machine</Button>
      </div>

      <Panel title="Overview">
        <StatRow>
          <StatTile label="connected machines" value={`${connectedCount} / ${nodes?.length ?? 0}`} />
          <StatTile label="lifetime earnings" value={money(me?.lifetime_earnings_micros)} />
        </StatRow>
      </Panel>

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
            emptyLabel="no machines yet — add one to start earning"
            columns={[
              { header: "hostname", render: (n) => n.hostname || n.id },
              { header: "id", render: (n) => <span className="font-mono text-xs text-muted">{n.id}</span> },
              { header: "status", render: (n) => <ConnectedBadge connected={n.connected} /> },
              { header: "trust", render: (n) => n.trust_score.toFixed(2) },
              { header: "last seen", render: (n) => relativeTime(n.last_heartbeat_at_ms) },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
