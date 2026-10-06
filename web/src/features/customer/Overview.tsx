import { Link } from "react-router-dom";
import { Activity, ArrowRight, CheckCircle2, Clock3, Layers, Network, Plus, Wallet } from "lucide-react";
import { apiGet } from "@/lib/api";
import { count, cpu, money } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { dailySpend } from "@/lib/spend";
import { costByTask } from "@/lib/tasks";
import type { Gateway, LedgerEntry, Me, Task } from "@/lib/types";
import { TaskTable } from "@/components/TaskTable";
import { BarChart } from "@/ui/BarChart";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardFooter, CardHeader } from "@/ui/Card";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier } from "@/ui/Identifier";
import { PageHeader } from "@/ui/PageHeader";
import { Skeleton } from "@/ui/Skeleton";
import { StatCard } from "@/ui/StatCard";
import { ConnectionPill } from "@/ui/StatusPill";
import { isActiveState } from "@/ui/status";

export function Overview() {
  usePageTitle("Overview");
  const me = useAsync((s) => apiGet<Me>("/api/portal/customer/me", s), []);
  const tasks = useAsync((s) => apiGet<Task[]>("/api/portal/customer/tasks", s), []);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/customer/ledger", s), []);
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);

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

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="Available balance"
          value={money(me.data?.available_balance_micros)}
          sub={
            me.data && me.data.balance_micros !== me.data.available_balance_micros
              ? `${money(me.data.balance_micros)} total, rest is held`
              : "Ready to spend"
          }
          icon={Wallet}
          tone="success"
          loading={me.loading}
        />
        <StatCard
          label="Running now"
          value={count(running.length)}
          sub={running.length ? `${cpu(coresInUse)} in use` : "Nothing running"}
          icon={Activity}
          tone="accent"
          loading={loading}
        />
        <StatCard
          label="In the queue"
          value={count(queued.length)}
          sub={queued.length ? "Waiting for capacity" : "Queue is empty"}
          icon={Clock3}
          loading={loading}
        />
        <StatCard
          label="Success rate"
          value={rate === null ? "—" : `${rate}%`}
          sub={rate === null ? "No finished tasks yet" : `${succeeded} of last ${finished.length} finished`}
          icon={CheckCircle2}
          tone={rate === null || rate >= 80 ? "success" : "warning"}
          loading={loading}
        />
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
              tasks={all.slice(0, 7)}
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

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader
            title="Spend"
            description="Last 7 days"
            action={
              <span className="text-sm font-semibold text-fg" data-tnum>
                {ledger.loading ? "" : money(spend.total)}
              </span>
            }
          />
          <CardBody>
            {ledger.loading ? (
              <Skeleton className="h-32 w-full" />
            ) : (
              <BarChart data={spend.bars} tone="accent" summary={`Spend over the last 7 days, ${money(spend.total)} in total`} />
            )}
          </CardBody>
          <CardFooter>
            <span className="text-xs text-muted">Charged when a task finishes</span>
            <Button asChild variant="ghost" size="sm">
              <Link to="/billing">Billing</Link>
            </Button>
          </CardFooter>
        </Card>

        <Card>
          <CardHeader
            title="Gateways"
            description="Your doors into your own infrastructure."
            action={
              <Button asChild variant="ghost" size="sm">
                <Link to="/gateways">Manage</Link>
              </Button>
            }
          />
          <CardBody className="pt-3">
            {gateways.loading ? (
              <div className="space-y-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : (gateways.data ?? []).length === 0 ? (
              <EmptyState
                compact
                icon={Network}
                title="No gateways"
                description="Install one next to your data to let tasks reach it."
                action={
                  <Button asChild variant="secondary" size="sm">
                    <Link to="/gateways">Add a gateway</Link>
                  </Button>
                }
              />
            ) : (
              <ul className="space-y-2.5">
                {(gateways.data ?? []).slice(0, 4).map((g) => (
                  <li key={g.id} className="flex min-w-0 items-center justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium text-fg">{g.label}</p>
                      <Identifier value={g.id} copy={false} className="text-muted" />
                    </div>
                    <ConnectionPill connected={g.connected} size="sm" />
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      </div>
    </div>
  );
}
