import { useMemo, useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router-dom";
import { Plus, Receipt } from "lucide-react";
import { apiGet, apiPost, errorMessage } from "@/lib/api";
import { money, relativeTime, shortDateTime, signedMoney } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { dailySpend } from "@/lib/spend";
import type { LedgerEntry, Me } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { BarChart } from "@/ui/BarChart";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { DataTable } from "@/ui/DataTable";
import { Dialog, DialogContent } from "@/ui/Dialog";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier } from "@/ui/Identifier";
import { KeyValueList } from "@/ui/KeyValue";
import { Field, Input } from "@/ui/Input";
import { PageHeader } from "@/ui/PageHeader";
import { Pagination } from "@/ui/Pagination";
import { Segmented } from "@/ui/Segmented";
import { Skeleton } from "@/ui/Skeleton";
import { useToast } from "@/ui/Toast";
import { cn } from "@/lib/cn";

const PRESETS = ["5", "10", "25", "50", "100"];
const PAGE_SIZE = 10;

function AddFundsDialog({ open, onOpenChange, onDone }: { open: boolean; onOpenChange: (o: boolean) => void; onDone: () => void }) {
  const toast = useToast();
  const [amount, setAmount] = useState("25");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dollars = Number(amount);
  const valid = Number.isFinite(dollars) && dollars > 0 && dollars <= 10_000;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await apiPost("/api/portal/customer/balance/add", { amount_micros: Math.round(dollars * 1e6) });
      toast.success("Funds added", `${money(Math.round(dollars * 1e6))} is available now.`);
      onDone();
      onOpenChange(false);
    } catch (err) {
      setError(errorMessage(err, "Couldn't add funds"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent
        title="Add funds"
        description="Top up your balance to run more tasks."
        footer={
          <>
            <Button variant="secondary" type="button" onClick={() => onOpenChange(false)} disabled={busy}>
              Cancel
            </Button>
            <Button type="submit" form="add-funds" loading={busy} disabled={!valid}>
              Add {valid ? money(Math.round(dollars * 1e6)) : "funds"}
            </Button>
          </>
        }
      >
        <form id="add-funds" onSubmit={submit} className="space-y-4 pb-2">
          <Alert tone="info" title="Test funds">
            There's no payment provider connected yet, so this adds credit instantly without charging anything.
          </Alert>
          {error && <Alert tone="danger">{error}</Alert>}
          <div className="grid grid-cols-5 gap-2" role="radiogroup" aria-label="Amount">
            {PRESETS.map((p) => (
              <button
                key={p}
                type="button"
                role="radio"
                aria-checked={amount === p}
                onClick={() => setAmount(p)}
                className={cn(
                  "h-10 rounded-lg border text-sm font-medium transition-colors",
                  amount === p ? "border-accent/60 bg-accent/10 text-accent" : "border-border bg-surface text-fg hover:border-border-strong"
                )}
              >
                ${p}
              </button>
            ))}
          </div>
          <Field label="Or enter an amount (USD)" error={amount !== "" && !valid ? "Enter an amount between $0.01 and $10,000." : null}>
            <Input leading="$" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^0-9.]/g, ""))} />
          </Field>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function Billing() {
  usePageTitle("Billing");
  const me = useAsync((s) => apiGet<Me>("/api/portal/customer/me", s), []);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/customer/ledger", s), []);
  const [funds, setFunds] = useState(false);
  const [page, setPage] = useState(1);
  const [range, setRange] = useState<"7" | "30">("7");

  useLiveReload(
    () => {
      me.reload();
      ledger.reload();
    },
    (e) => e.type === "balance" || e.type === "task_state"
  );

  const entries = useMemo(() => [...(ledger.data ?? [])].sort((a, b) => b.created_at_ms - a.created_at_ms), [ledger.data]);
  const spend = dailySpend(ledger.data, Number(range));
  const visible = entries.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);
  const held = me.data ? me.data.balance_micros - me.data.available_balance_micros : 0;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Billing"
        description="Your balance, what you've spent, and every charge."
        actions={
          <Button onClick={() => setFunds(true)}>
            <Plus />
            Add funds
          </Button>
        }
      />

      <div className="grid items-stretch gap-6 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        <Card>
          <CardBody className="space-y-5">
            <div className="space-y-1">
              <p className="text-xs font-medium text-muted">Available balance</p>
              {me.loading ? (
                <Skeleton className="mt-2 h-10 w-40" />
              ) : (
                <p className="truncate text-4xl font-semibold tracking-tight text-fg" data-tnum>
                  {money(me.data?.available_balance_micros)}
                </p>
              )}
            </div>
            <KeyValueList
              items={[
                { label: "Total balance", value: <span data-tnum>{me.data ? money(me.data.balance_micros) : "…"}</span> },
                { label: "Held", value: <span data-tnum>{me.data ? money(held) : "…"}</span> },
                { label: `Spent (${range}d)`, value: <span data-tnum>{ledger.data ? money(spend.total) : "…"}</span> },
              ]}
            />
            <p className="text-xs leading-5 text-muted">
              Running tasks hold their worst-case cost up front; whatever isn't used is released when they finish.
            </p>
          </CardBody>
        </Card>

        <Card>
          <CardHeader
            title="Spend"
            description={`${money(spend.total)} in the last ${range} days`}
            action={
              <Segmented
                label="Time range"
                value={range}
                onChange={(v) => setRange(v)}
                options={[
                  { value: "7", label: "7 days" },
                  { value: "30", label: "30 days" },
                ]}
              />
            }
          />
          <CardBody>
            {ledger.loading ? (
              <Skeleton className="h-36 w-full" />
            ) : (
              <BarChart
                data={spend.bars}
                heightClass="h-36"
                summary={`Spend over the last ${range} days, ${money(spend.total)} in total`}
              />
            )}
          </CardBody>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <CardHeader title="Transactions" description="Charged when a task finishes, at the normalised rate." className="pb-4" />
        <div className="border-t border-border">
          {ledger.error && !ledger.data ? (
            <ErrorState title="Couldn't load transactions" message={ledger.error} onRetry={ledger.reload} compact />
          ) : (
            <>
              <DataTable
                caption="Transactions"
                loading={ledger.loading}
                rows={visible}
                getRowKey={(e) => e.id}
                minWidth={560}
                empty={
                  <EmptyState
                    icon={Receipt}
                    title="No transactions yet"
                    description="Charges appear here as soon as a task finishes."
                    action={
                      <Button asChild variant="secondary">
                        <Link to="/tasks/new">Submit a task</Link>
                      </Button>
                    }
                  />
                }
                columns={[
                  {
                    id: "when",
                    header: "When",
                    width: "11rem",
                    cell: (e) => (
                      <span className="text-muted" title={shortDateTime(e.created_at_ms)} data-tnum>
                        {relativeTime(e.created_at_ms)}
                      </span>
                    ),
                  },
                  {
                    id: "kind",
                    header: "Type",
                    width: "7.5rem",
                    cell: (e) => (
                      <Badge tone={e.kind === "charge" ? "neutral" : "success"} size="sm">
                        {e.kind === "charge" ? "Charge" : "Credit"}
                      </Badge>
                    ),
                  },
                  {
                    id: "task",
                    header: "Task",
                    cell: (e) => (
                      <Link to={`/tasks/${e.task_id}`} className="inline-block max-w-full hover:underline">
                        <Identifier value={e.task_id} copy={false} />
                      </Link>
                    ),
                  },
                  {
                    id: "amount",
                    header: "Amount",
                    width: "8rem",
                    align: "right",
                    cell: (e) => (
                      <span className={cn("font-medium", e.kind === "credit" ? "text-success" : "text-fg")} data-tnum>
                        {signedMoney(e.amount_micros, e.kind === "credit" ? 1 : -1)}
                      </span>
                    ),
                  },
                ]}
                mobileCard={(e) => (
                  <div className="flex min-w-0 items-center justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <Identifier value={e.task_id} copy={false} />
                      <p className="text-xs text-muted">{relativeTime(e.created_at_ms)}</p>
                    </div>
                    <span className={cn("shrink-0 text-sm font-medium", e.kind === "credit" ? "text-success" : "text-fg")} data-tnum>
                      {signedMoney(e.amount_micros, e.kind === "credit" ? 1 : -1)}
                    </span>
                  </div>
                )}
              />
              <Pagination page={page} pageSize={PAGE_SIZE} total={entries.length} onChange={setPage} />
            </>
          )}
        </div>
      </Card>

      <AddFundsDialog
        open={funds}
        onOpenChange={setFunds}
        onDone={() => {
          me.reload();
          ledger.reload();
        }}
      />
    </div>
  );
}
