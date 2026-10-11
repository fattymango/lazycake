import { Link } from "react-router-dom";
import { Activity, ArrowRight, CheckCircle2, Clock3, Layers, Plus, Wallet } from "lucide-react";
import { apiGet } from "@/lib/api";
import { count, cpu, money } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { dailySpend } from "@/lib/spend";
import { costByTask } from "@/lib/tasks";
import type { LedgerEntry, Me, Task } from "@/lib/types";
import { TaskTable } from "@/components/TaskTable";
import { BarChart } from "@/ui/BarChart";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardFooter, CardHeader } from "@/ui/Card";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { PageHeader } from "@/ui/PageHeader";
import { Skeleton } from "@/ui/Skeleton";
import { RingGauge } from "@/ui/RingGauge";
import { isActiveState } from "@/ui/status";

export function Overview() {
  usePageTitle("Overview");
  const me = useAsync((s) => apiGet<Me>("/api/portal/customer/me", s), []);
  const tasks = useAsync((s) => apiGet<Task[]>("/api/portal/customer/tasks", s), []);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/customer/ledger", s), []);

  useLiveReload(
    () => {
      tasks.reload();
      ledger.reload();
      me.reload();
    },
    (e) => e.type === "task_state" || e.type === "balance"
  );

  const all = tasks.data ?? [];
  const running = all.filter((t) => t.state === "running" || t.state === "dispatched");
  const queued = all.filter((t) => t.state === "queued" || t.state === "reserved");
  const finished = all.filter((t) => !isActiveState(t.state)).slice(0, 50);
  const succeeded = finished.filter((t) => t.state === "succeeded").length;
  const rate = finished.length ? Math.round((succeeded / finished.length) * 100) : null;
  const coresInUse = running.reduce((n, t) => n + t.cores, 0);
  const costs = costByTask(ledger.data);
  const spend = dailySpend(ledger.data, 7);

  const loading = tasks.loading;
  const hasError = tasks.error && !tasks.data;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Overview"
        description="What's running, what it costs, and what needs attention."
        actions={
          <Button asChild>
            <Link to="/tasks/new">
              <Plus />
              New task
            </Link>
          </Button>
        }
      />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Card className="flex flex-col">
          <CardBody className="grid flex-1 gap-6 sm:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
            <div className="min-w-0 space-y-4">
              <div>
                <p className="text-xs font-medium text-muted">Available balance</p>
                {me.loading ? (
                  <Skeleton className="mt-2 h-10 w-40" />
                ) : (
                  <p className="mt-1 truncate text-4xl font-semibold tracking-tight text-fg" data-tnum>
                    {money(me.data?.available_balance_micros)}
                  </p>
                )}
                <p className="mt-1 text-xs text-muted">
                  {me.data && me.data.balance_micros !== me.data.available_balance_micros
                    ? `${money(me.data.balance_micros)} total, the rest is held for running tasks`
                    : "Ready to spend"}
                </p>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <Wallet className="size-4 text-success" aria-hidden />
                <span className="text-muted">Spent, last 7 days</span>
                <span className="ml-auto font-semibold text-fg" data-tnum>
                  {ledger.loading ? "…" : money(spend.total)}
                </span>
              </div>
            </div>
            <div className="min-w-0">
              {ledger.loading ? (
                <Skeleton className="h-36 w-full" />
              ) : (
                <BarChart
                  data={spend.bars}
                  tone="accent"
                  heightClass="h-36"
                  summary={`Spend over the last 7 days, ${money(spend.total)} in total`}
                />
              )}
            </div>
          </CardBody>
          <CardFooter>
            <span className="text-xs text-muted">Charged when a task finishes</span>
            <Button asChild variant="ghost" size="sm">
              <Link to="/billing">Billing</Link>
            </Button>
          </CardFooter>
        </Card>

        <Card className="flex flex-col">
          <CardBody className="flex flex-1 flex-wrap items-center gap-x-8 gap-y-4">
            {loading ? (
              <Skeleton className="size-28 rounded-full" />
            ) : (
              <RingGauge
                value={rate === null ? null : rate / 100}
                tone={rate === null || rate >= 80 ? "success" : "warning"}
                center={
                  <div>
                    <p className="text-2xl font-semibold leading-7 text-fg" data-tnum>
                      {rate === null ? "—" : `${rate}%`}
                    </p>
                    <p className="text-2xs text-muted">succeeded</p>
                  </div>
                }
                summary={
                  rate === null ? "No finished tasks yet" : `${rate} percent of the last ${finished.length} finished tasks succeeded`
                }
              />
            )}
            <dl className="min-w-0 flex-1 space-y-3 text-sm">
              <div className="flex items-center justify-between gap-3">
                <dt className="flex items-center gap-2 text-muted">
                  <Activity className="size-4 text-accent" aria-hidden />
                  Running now
                </dt>
                <dd className="font-semibold text-fg" data-tnum>
                  {loading ? "…" : count(running.length)}
                  {running.length > 0 && <span className="ml-2 text-xs font-normal text-muted">{cpu(coresInUse)} in use</span>}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="flex items-center gap-2 text-muted">
                  <Clock3 className="size-4" aria-hidden />
                  In the queue
                </dt>
                <dd className="font-semibold text-fg" data-tnum>
                  {loading ? "…" : count(queued.length)}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="flex items-center gap-2 text-muted">
                  <CheckCircle2 className="size-4 text-success" aria-hidden />
                  Finished
                </dt>
                <dd className="text-fg" data-tnum>
                  {loading ? "…" : rate === null ? "None yet" : `${succeeded} of last ${finished.length}`}
                </dd>
              </div>
            </dl>
          </CardBody>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <CardHeader
          title="Recent tasks"
          description="Your latest submissions, newest first."
          action={
            <Button asChild variant="ghost" size="sm">
              <Link to="/tasks">
                View all
                <ArrowRight />
              </Link>
            </Button>
          }
          className="pb-4"
        />
        <div className="border-t border-border">
          {hasError ? (
            <ErrorState title="Couldn't load your tasks" message={tasks.error} onRetry={tasks.reload} compact />
          ) : (
            <TaskTable
              tasks={all.slice(0, 12)}
              costs={costs}
              loading={loading}
              skeletonRows={5}
              empty={
                <EmptyState
                  icon={Layers}
                  title="No tasks yet"
                  description="Submit a container and watch it queue, run on a host's machine, and settle here."
                  action={
                    <Button asChild>
                      <Link to="/tasks/new">
                        <Plus />
                        Submit your first task
                      </Link>
                    </Button>
                  }
                />
              }
            />
          )}
        </div>
      </Card>
    </div>
  );
}
