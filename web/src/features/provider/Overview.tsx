import { Link } from "react-router-dom";
import { Cpu, HardDrive, MemoryStick, Plus, Server, ShieldCheck, Wallet, Wifi } from "lucide-react";
import { apiGet } from "@/lib/api";
import { cores, cpu, memory, money, relativeTime, count } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { dailyEarnings } from "@/lib/spend";
import type { LedgerEntry, Node, NodeUsage, ProviderMe } from "@/lib/types";
import { TrustMeter } from "@/components/TrustMeter";
import { BarChart } from "@/ui/BarChart";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { PageHeader } from "@/ui/PageHeader";
import { Sparkline } from "@/ui/Sparkline";
import { Skeleton } from "@/ui/Skeleton";
import { StatCard } from "@/ui/StatCard";
import { ConnectionPill } from "@/ui/StatusPill";
import { Truncate } from "@/ui/Identifier";

function Spec({ icon: Icon, label, value }: { icon: typeof Cpu; label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-wider text-muted">
        <Icon className="size-3" aria-hidden />
        {label}
      </p>
      <p className="mt-1 truncate text-sm font-medium text-fg" data-tnum>
        {value}
      </p>
    </div>
  );
}

/** A day of CPU use by tasks, as a line, with the current figure next to it. */
function UsageTrend({ node }: { node: Node }) {
  const usage = useAsync(
    (s) => apiGet<NodeUsage>(`/api/portal/provider/nodes/${encodeURIComponent(node.id)}/usage?range=24h`, s),
    [node.id]
  );
  const u = usage.data;
  if (!u || !u.supported) return null;
  const now = u.latest && node.connected ? u.latest.tasks_cpu_cores : undefined;
  return (
    <div className="space-y-1.5" aria-label="CPU used by tasks over the last 24 hours">
      <div className="flex items-center justify-between text-xs text-muted">
        <span>CPU in use, 24 h</span>
        <span className="text-fg" data-tnum>
          {now === undefined ? "—" : `${cores(now)} of ${node.offer_cores}`}
        </span>
      </div>
      <Sparkline data={u.series.map((p) => p.tasks_cpu_cores)} />
    </div>
  );
}

export function MachineCard({ node }: { node: Node }) {
  return (
    <Link to={`/machines/${node.id}`} className="group block min-w-0 rounded-xl focus-visible:outline-offset-4">
      <Card interactive className="flex h-full flex-col gap-5 p-5">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-border bg-raised text-muted transition-colors group-hover:text-accent">
              <Server className="size-5" aria-hidden />
            </span>
            <div className="min-w-0">
              <h3 className="min-w-0 text-sm font-semibold text-fg">
                <Truncate>{node.hostname}</Truncate>
              </h3>
              <Badge tone="neutral" size="sm" className="mt-1 font-mono">
                {node.arch}
              </Badge>
            </div>
          </div>
          <ConnectionPill connected={node.connected} size="sm" />
        </div>

        <div className="grid grid-cols-3 gap-3">
          <Spec icon={Cpu} label="CPU" value={cpu(node.offer_cores)} />
          <Spec icon={MemoryStick} label="RAM" value={memory(node.offer_memory_mb)} />
          <Spec icon={HardDrive} label="Disk" value={memory(node.offer_disk_mb)} />
        </div>

        <UsageTrend node={node} />

        <div className="mt-auto space-y-2">
          <div className="flex items-center justify-between text-xs text-muted">
            <span>Trust</span>
            <span>
              {node.connected ? "Heartbeat " : "Last seen "}
              {relativeTime(node.last_heartbeat_at_ms)}
            </span>
          </div>
          <TrustMeter score={node.trust_score} />
        </div>
      </Card>
    </Link>
  );
}

export function Overview() {
  usePageTitle("Overview");
  const nodes = useAsync((s) => apiGet<Node[]>("/api/portal/provider/nodes", s), []);
  const me = useAsync((s) => apiGet<ProviderMe>("/api/portal/provider/me", s), []);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/provider/ledger", s), []);

  useLiveReload(
    () => {
      nodes.reload();
      me.reload();
      ledger.reload();
    },
    (e) => e.type === "node_connected" || e.type === "node_disconnected" || e.type === "task_state"
  );

  const list = nodes.data ?? [];
  const online = list.filter((n) => n.connected);
  const onlineCores = online.reduce((n, x) => n + x.offer_cores, 0);
  const onlineMem = online.reduce((n, x) => n + x.offer_memory_mb, 0);
  const avgTrust = list.length ? list.reduce((n, x) => n + x.trust_score, 0) / list.length : null;
  const earnings = dailyEarnings(ledger.data, 14);
  const addButton = (
    <Button asChild>
      <Link to="/machines/add">
        <Plus />
        Add a machine
      </Link>
    </Button>
  );

  return (
    <div className="space-y-6">
      <PageHeader title="Overview" description="Your machines, what they're offering, and what they've earned." actions={addButton} />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="Lifetime earnings"
          value={money(me.data?.lifetime_earnings_micros)}
          sub="Across all machines"
          icon={Wallet}
          tone="success"
          loading={me.loading}
          trend={earnings.total > 0 ? earnings.bars.map((b) => b.value) : undefined}
        />
        <StatCard
          label="Machines online"
          value={nodes.loading ? "" : `${online.length} / ${list.length}`}
          sub={
            list.length === 0
              ? "None registered yet"
              : online.length === list.length
                ? "All connected"
                : `${list.length - online.length} offline`
          }
          icon={Wifi}
          tone={online.length > 0 ? "accent" : "neutral"}
          loading={nodes.loading}
        />
        <StatCard
          label="Capacity on offer"
          value={cpu(onlineCores)}
          sub={`${memory(onlineMem)} RAM, online now`}
          icon={Cpu}
          loading={nodes.loading}
        />
        <StatCard
          label="Average trust"
          value={avgTrust === null ? "—" : avgTrust.toFixed(2)}
          sub={avgTrust === null ? "Builds as machines complete work" : "Out of 1.00"}
          icon={ShieldCheck}
          tone={avgTrust === null ? "neutral" : avgTrust >= 0.8 ? "success" : "warning"}
          loading={nodes.loading}
        />
      </div>

      <section className="space-y-3" aria-labelledby="machines-h">
        <div className="flex items-center justify-between">
          <h2 id="machines-h" className="text-sm font-semibold text-fg">
            Machines
          </h2>
          {list.length > 0 && <span className="text-xs text-muted">{count(list.length)} registered</span>}
        </div>
        {nodes.error && !nodes.data ? (
          <Card>
            <ErrorState title="Couldn't load your machines" message={nodes.error} onRetry={nodes.reload} />
          </Card>
        ) : nodes.loading ? (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-52 rounded-xl" />
            ))}
          </div>
        ) : list.length === 0 ? (
          <Card>
            <EmptyState
              icon={Server}
              title="No machines yet"
              description="Register a computer and it starts earning whenever a customer's task runs on it. It takes about two minutes."
              action={addButton}
            />
          </Card>
        ) : (
          <ul className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {list.map((n) => (
              <li key={n.id} className="min-w-0">
                <MachineCard node={n} />
              </li>
            ))}
          </ul>
        )}
      </section>

      {list.length > 0 && (
        <Card>
          <CardHeader
            title="Earnings"
            description="Last 14 days"
            action={
              <span className="text-sm font-semibold text-fg" data-tnum>
                {money(earnings.total)}
              </span>
            }
          />
          <CardBody>
            {ledger.loading ? (
              <Skeleton className="h-32 w-full" />
            ) : (
              <BarChart data={earnings.bars} tone="success" summary={`Earnings over the last 14 days, ${money(earnings.total)} in total`} />
            )}
          </CardBody>
        </Card>
      )}
    </div>
  );
}
