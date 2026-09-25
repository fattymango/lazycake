import { useEffect, useState } from "react";
import { apiGet, apiPost, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { Button, Field, TextInput } from "@shared/components/Form";
import { StatRow, StatTile } from "@shared/components/StatTile";
import { dateTime, money } from "@shared/format";
import { useCustomerAuth } from "@shared/auth";
import type { LedgerEntry } from "@shared/types";

export function Billing() {
  const { me, refresh } = useCustomerAuth();
  const [entries, setEntries] = useState<LedgerEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [addAmount, setAddAmount] = useState("10");
  const [adding, setAdding] = useState(false);

  async function load() {
    setError(null);
    try {
      setEntries(await apiGet<LedgerEntry[]>("/api/portal/customer/ledger"));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load ledger");
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function handleAddFunds() {
    setAdding(true);
    try {
      const micros = Math.round(parseFloat(addAmount) * 1e6);
      await apiPost("/api/portal/customer/balance/add", { amount_micros: micros });
      await Promise.all([load(), refresh()]);
    } catch {
      // surfaced via ErrorState on next load if it's a real problem
    } finally {
      setAdding(false);
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-xl font-semibold">Billing</h1>

      <Panel title="Balance">
        <StatRow>
          <StatTile label="available balance" value={money(me?.available_balance_micros)} />
          <StatTile label="account balance" value={money(me?.balance_micros)} />
        </StatRow>
      </Panel>

      <Panel title="Add funds (dev only)" className="max-w-sm">
        <p className="text-xs text-warn mb-4">
          No real payment rail exists yet — this credits your account directly for testing.
        </p>
        <Field label="Amount (USD)">
          <TextInput type="number" min="0" step="0.01" value={addAmount} onChange={(e) => setAddAmount(e.target.value)} />
        </Field>
        <Button onClick={handleAddFunds} disabled={adding}>
          {adding ? "adding…" : "Add funds"}
        </Button>
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
            emptyLabel="no ledger entries yet"
            columns={[
              { header: "kind", render: (e) => e.kind },
              { header: "task", render: (e) => <span className="font-mono text-xs text-muted">{e.task_id}</span> },
              {
                header: "amount",
                render: (e) => (
                  <span className={e.kind === "charge" ? "text-bad" : "text-good"}>
                    {e.kind === "charge" ? "-" : "+"}
                    {money(Math.abs(e.amount_micros))}
                  </span>
                ),
              },
              { header: "date", render: (e) => dateTime(e.created_at_ms) },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
