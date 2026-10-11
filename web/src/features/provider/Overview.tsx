import { Link } from "react-router-dom";
import { Cpu, Gauge, HardDrive, MemoryStick, Plus, Server, ShieldCheck, Wallet, Wifi } from "lucide-react";
import { apiGet } from "@/lib/api";
import { cores, cpu, memory, money, relativeTime, count } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { dailyEarnings } from "@/lib/spend";
import type { LedgerEntry, Node, NodeUsage, ProviderMe } from "@/lib/types";
import { TrustMeter } from "@/components/TrustMeter";
import { TrustInfo } from "@/components/TrustInfo";
import { BarChart } from "@/ui/BarChart";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardFooter } from "@/ui/Card";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { PageHeader } from "@/ui/PageHeader";
import { Sparkline } from "@/ui/Sparkline";
import { Skeleton } from "@/ui/Skeleton";
import { RingGauge } from "@/ui/RingGauge";
import { ConnectionPill } from "@/ui/StatusPill";
import { Truncate } from "@/ui/Identifier";
import { updateLabel } from "@/lib/update";

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
          <span className="flex shrink-0 flex-col items-end gap-1.5">
            <ConnectionPill connected={node.connected} size="sm" />
            {updateLabel(node.update_state) && (
              <Badge tone="warning" size="sm">
                {updateLabel(node.update_state)}
              </Badge>
            )}
          </span>
        </div>

        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Spec icon={Cpu} label="CPU" value={cpu(node.offer_cores)} />
          <Spec icon={MemoryStick} label="RAM" value={memory(node.offer_memory_mb)} />
          <Spec icon={HardDrive} label="Disk" value={memory(node.offer_disk_mb)} />
          <Spec icon={Gauge} label="Net" value={node.offer_network_mbps > 0 ? `${node.offer_network_mbps} Mbps` : "—"} />
        </div>

        <UsageTrend node={node} />

        <div className="mt-auto space-y-2">
          <div className="flex items-center justify-between text-xs text-muted">
            <span className="flex items-center gap-1.5">
              Trust <TrustInfo />
            </span>
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

      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Card className="flex flex-col">
          <CardBody className="grid flex-1 gap-6 sm:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
            <div className="min-w-0 space-y-4">
              <div>
                <p className="text-xs font-medium text-muted">Lifetime earnings</p>
                {me.loading ? (
                  <Skeleton className="mt-2 h-10 w-40" />
                ) : (
                  <p className="mt-1 truncate text-4xl font-semibold tracking-tight text-fg" data-tnum>
                    {money(me.data?.lifetime_earnings_micros)}
                  </p>
                )}
                <p className="mt-1 text-xs text-muted">Across all machines</p>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <Wallet className="size-4 text-success" aria-hidden />
                <span className="text-muted">Earned, last 14 days</span>
                <span className="ml-auto font-semibold text-fg" data-tnum>
                  {ledger.loading ? "…" : money(earnings.total)}
                </span>
              </div>
            </div>
            <div className="min-w-0">
              {ledger.loading ? (
                <Skeleton className="h-36 w-full" />
              ) : (
                <BarChart
                  data={earnings.bars}
                  tone="success"
                  heightClass="h-36"
                  summary={`Earnings over the last 14 days, ${money(earnings.total)} in total`}
                />
              )}
            </div>
          </CardBody>
          <CardFooter>
            <span className="text-xs text-muted">Credited when a task finishes on one of your machines</span>
            <Button asChild variant="ghost" size="sm">
              <Link to="/earnings">Earnings</Link>
            </Button>
          </CardFooter>
        </Card>

        <Card className="flex flex-col">
          <CardBody className="flex flex-1 flex-wrap items-center gap-x-8 gap-y-4">
            {nodes.loading ? (
              <Skeleton className="size-28 rounded-full" />
            ) : (
              <RingGauge
                value={list.length ? online.length / list.length : null}
                tone={
                  list.length === 0 ? "neutral" : online.length === list.length ? "success" : online.length === 0 ? "danger" : "warning"
                }
                center={
                  <div>
                    <p className="text-2xl font-semibold leading-7 text-fg" data-tnum>
                      {online.length} / {list.length}
                    </p>
                    <p className="text-2xs text-muted">online</p>
                  </div>
                }
                summary={`${online.length} of ${list.length} machines online`}
              />
            )}
            <dl className="min-w-0 flex-1 space-y-3 text-sm">
              <div className="flex items-center justify-between gap-3">
                <dt className="flex items-center gap-2 text-muted">
                  <Wifi className="size-4 text-accent" aria-hidden />
                  Machines
                </dt>
                <dd className="text-fg" data-tnum>
                  {nodes.loading
                    ? "…"
                    : list.length === 0
                      ? "None registered"
                      : online.length === list.length
                        ? "All connected"
                        : `${list.length - online.length} offline`}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="flex items-center gap-2 text-muted">
                  <Cpu className="size-4" aria-hidden />
                  On offer now
                </dt>
                <dd className="text-fg" data-tnum>
                  {nodes.loading ? "…" : `${cpu(onlineCores)} · ${memory(onlineMem)}`}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="flex items-center gap-2 text-muted">
                  <ShieldCheck className="size-4 text-success" aria-hidden />
                  <span className="inline-flex items-center gap-1.5">
                    Average trust <TrustInfo />
                  </span>
                </dt>
                <dd className="font-semibold text-fg" data-tnum>
                  {nodes.loading ? "…" : avgTrust === null ? "—" : `${avgTrust.toFixed(2)} / 1.00`}
                </dd>
              </div>
            </dl>
          </CardBody>
        </Card>
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
          <div className="grid grid-cols-[repeat(auto-fill,minmax(20rem,1fr))] gap-4">
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
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(20rem,1fr))] gap-4">
            {list.map((n) => (
              <li key={n.id} className="min-w-0">
                <MachineCard node={n} />
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
