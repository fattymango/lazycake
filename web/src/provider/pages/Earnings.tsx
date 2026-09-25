import { useEffect, useState } from "react";
import { apiGet, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { dateTime, money } from "@shared/format";
import { useProviderAuth } from "@shared/auth";
import type { LedgerEntry } from "@shared/types";

export function Earnings() {
  const { me } = useProviderAuth();
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
      <h1 className="text-xl font-semibold">Earnings</h1>

      <Panel title="Lifetime">
        <StatRow>
          <StatTile label="lifetime earnings" value={money(me?.lifetime_earnings_micros)} />
        </StatRow>
      </Panel>

      <Panel title="Ledger history">
        {entries === null && !error ? (
          <LoadingState />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : (
          <DataTable
            rows={entries!}
            rowKey={(e) => e.id}
            emptyLabel="no earnings yet — connect a machine and pick up a task"
            columns={[
              { header: "task", render: (e) => <span className="font-mono text-xs text-muted">{e.task_id}</span> },
              { header: "amount", render: (e) => <span className="text-good">+{money(e.amount_micros)}</span> },
              { header: "date", render: (e) => dateTime(e.created_at_ms) },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
