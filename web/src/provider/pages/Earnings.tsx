import { useEffect, useState } from "react";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { IconEarnings, IconHistory } from "@shared/components/Icon";
import { dateTime, money } from "@shared/format";
import { useRoleMe } from "@shared/hooks/useRoleMe";
import type { LedgerEntry, ProviderMe } from "@shared/types";

export function Earnings() {
  const { data: me } = useRoleMe<ProviderMe>("/api/portal/provider/me");
  const [entries, setEntries] = useState<LedgerEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setError(null);
    try {
      setEntries(await apiGet<LedgerEntry[]>("/api/portal/provider/ledger"));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load earnings");
    }
  }

  useEffect(() => {
    load();
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold">Earnings</h1>
        <p className="text-sm text-muted mt-0.5">Every payout your machines have earned.</p>
      </div>

      <StatRow>
        <StatTile icon={IconEarnings} label="Lifetime earnings" value={money(me?.lifetime_earnings_micros)} tone="good" />
      </StatRow>

      <Panel title="Ledger history">
        {entries === null && !error ? (
          <LoadingState />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : (
          <DataTable
            rows={entries!}
            rowKey={(e) => e.id}
            emptyLabel="No earnings yet - connect a machine and pick up a task."
            emptyIcon={IconHistory}
            columns={[
              { header: "Task", render: (e) => <span className="font-mono text-xs text-muted">{e.task_id}</span> },
              { header: "Amount", render: (e) => <span className="text-good">+{money(e.amount_micros)}</span> },
              { header: "Date", render: (e) => <span className="text-muted">{dateTime(e.created_at_ms)}</span> },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
