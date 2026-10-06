import { useMemo, useState } from "react";
import { Coins, Wallet } from "lucide-react";
import { apiGet } from "@/lib/api";
import { cn } from "@/lib/cn";
import { count, money, relativeTime, shortDateTime, signedMoney } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { dailyEarnings } from "@/lib/spend";
import type { LedgerEntry, ProviderMe } from "@/lib/types";
import { BarChart } from "@/ui/BarChart";
import { Badge } from "@/ui/Badge";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { DataTable } from "@/ui/DataTable";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier } from "@/ui/Identifier";
import { PageHeader } from "@/ui/PageHeader";
import { Pagination } from "@/ui/Pagination";
import { Segmented } from "@/ui/Segmented";
import { Skeleton } from "@/ui/Skeleton";
import { StatCard } from "@/ui/StatCard";

const PAGE_SIZE = 10;

export function Earnings() {
  usePageTitle("Earnings");
  const me = useAsync((s) => apiGet<ProviderMe>("/api/portal/provider/me", s), []);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/provider/ledger", s), []);
  const [range, setRange] = useState<"14" | "30">("14");
  const [page, setPage] = useState(1);

  useLiveReload(
    () => {
      me.reload();
      ledger.reload();
    },
    (e) => e.type === "task_state"
  );

  const entries = useMemo(() => [...(ledger.data ?? [])].sort((a, b) => b.created_at_ms - a.created_at_ms), [ledger.data]);
  const earnings = dailyEarnings(ledger.data, Number(range));
  const visible = entries.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);
  const avg = entries.length ? entries.reduce((n, e) => n + e.amount_micros, 0) / entries.length : 0;

  return (
    <div className="space-y-6">
      <PageHeader title="Earnings" description="What your machines have earned, task by task." />

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          label="Lifetime earnings"
          value={money(me.data?.lifetime_earnings_micros)}
          icon={Wallet}
          tone="success"
          loading={me.loading}
        />
        <StatCard label={`Last ${range} days`} value={money(earnings.total)} icon={Coins} tone="accent" loading={ledger.loading} />
        <StatCard
          label="Paid tasks"
          value={count(entries.length)}
          sub={entries.length ? `${money(avg)} average` : undefined}
          icon={Coins}
          loading={ledger.loading}
        />
      </div>

      <Card>
        <CardHeader
          title="Daily earnings"
          action={
            <Segmented
              label="Time range"
              value={range}
              onChange={setRange}
              options={[
                { value: "14", label: "14 days" },
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
              data={earnings.bars}
              tone="success"
              heightClass="h-36"
              summary={`Earnings over the last ${range} days, ${money(earnings.total)} in total`}
            />
          )}
        </CardBody>
      </Card>

      <Card className="overflow-hidden">
        <CardHeader title="Payments" description="Credited when a task finishes on one of your machines." className="pb-4" />
        <div className="border-t border-border">
          {ledger.error && !ledger.data ? (
            <ErrorState title="Couldn't load payments" message={ledger.error} onRetry={ledger.reload} compact />
          ) : (
            <>
              <DataTable
                caption="Payments"
                loading={ledger.loading}
                rows={visible}
                getRowKey={(e) => e.id}
                minWidth={520}
                empty={
                  <EmptyState
                    icon={Coins}
                    title="No earnings yet"
                    description="Payments appear here as customers' tasks finish on your machines."
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
                    cell: () => (
                      <Badge tone="success" size="sm">
                        Credit
                      </Badge>
                    ),
                  },
                  { id: "task", header: "Task", cell: (e) => <Identifier value={e.task_id} /> },
                  {
                    id: "amount",
                    header: "Amount",
                    width: "8rem",
                    align: "right",
                    cell: (e) => (
                      <span className={cn("font-medium text-success")} data-tnum>
                        {signedMoney(e.amount_micros, 1)}
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
                    <span className="shrink-0 text-sm font-medium text-success" data-tnum>
                      {signedMoney(e.amount_micros, 1)}
                    </span>
                  </div>
                )}
              />
              <Pagination page={page} pageSize={PAGE_SIZE} total={entries.length} onChange={setPage} />
            </>
          )}
        </div>
      </Card>
    </div>
  );
}
