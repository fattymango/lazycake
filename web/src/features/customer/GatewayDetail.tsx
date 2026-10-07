import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ArrowDownToLine, ArrowUpFromLine, Plug } from "lucide-react";
import { apiGet } from "@/lib/api";
import { bytes, count, shortDateTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useInterval } from "@/lib/hooks/useInterval";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { fillBuckets, type StackPoint } from "@/lib/series";
import type { Gateway, GatewayTraffic } from "@/lib/types";
import { Badge } from "@/ui/Badge";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { DataTable, type Column } from "@/ui/DataTable";
import { ErrorState } from "@/ui/EmptyState";
import { Identifier } from "@/ui/Identifier";
import { NotFound } from "@/ui/NotFound";
import { PageHeader } from "@/ui/PageHeader";
import { Segmented } from "@/ui/Segmented";
import { Skeleton } from "@/ui/Skeleton";
import { StatCard } from "@/ui/StatCard";
import { ConnectionPill } from "@/ui/StatusPill";
import { StackedChart, type ChartSeries } from "@/ui/StackedChart";
import { TrafficLegend } from "./TrafficLegend";

type Range = "7d" | "30d";
const WINDOW: Record<Range, number> = { "7d": 7 * 86_400_000, "30d": 30 * 86_400_000 };

const SERIES: ChartSeries[] = [
  { key: "received", label: "Received from tasks", className: "bg-accent" },
  { key: "sent", label: "Sent to tasks", className: "bg-success" },
];

type Busiest = GatewayTraffic["busiest_tasks"][number];

export function GatewayDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const [range, setRange] = useState<Range>("7d");
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);
  const traffic = useAsync(
    (s) => apiGet<GatewayTraffic>(`/api/portal/customer/gateways/${encodeURIComponent(id)}/traffic?range=${range}`, s),
    [id, range]
  );
  useInterval(() => {
    gateways.reload();
    traffic.reload();
  }, 30_000);

  const gw = gateways.data?.find((g) => g.id === id);
  usePageTitle(gw ? `Gateway ${gw.label}` : "Gateway");

  const points = useMemo<StackPoint[]>(() => {
    if (!traffic.data) return [];
    const raw = traffic.data.series.map((p) => ({
      at: p.at_ms,
      values: { received: p.received_from_tasks_bytes, sent: p.sent_to_tasks_bytes },
    }));
    return fillBuckets(raw, traffic.data.step, WINDOW[traffic.data.range]);
  }, [traffic.data]);

  if (traffic.errorStatus === 404 || (gateways.data && !gw)) {
    return (
      <NotFound
        title="Gateway not found"
        description="This gateway doesn't exist, or it belongs to a different account."
        backTo="/gateways"
        backLabel="Back to gateways"
      />
    );
  }
  if (traffic.error && !traffic.data)
    return <ErrorState title="Couldn't load this gateway's traffic" message={traffic.error} onRetry={traffic.reload} />;

  const totals = traffic.data?.totals ?? gw?.traffic;
  const step = traffic.data?.step ?? "hour";
  const periodTotal = points.reduce((a, p) => ({ received: a.received + (p.values.received ?? 0), sent: a.sent + (p.values.sent ?? 0) }), {
    received: 0,
    sent: 0,
  });

  const columns: Column<Busiest>[] = [
    {
      id: "task",
      header: "Task",
      cell: (b) => (
        <Link to={`/tasks/${b.task_id}`} className="text-accent hover:underline">
          <Identifier value={b.task_id} />
        </Link>
      ),
    },
    {
      id: "received",
      header: "Received from tasks",
      width: "11rem",
      align: "right",
      cell: (b) => <span data-tnum>{bytes(b.received_from_tasks_bytes)}</span>,
    },
    {
      id: "sent",
      header: "Sent to tasks",
      width: "9rem",
      align: "right",
      cell: (b) => <span data-tnum>{bytes(b.sent_to_tasks_bytes)}</span>,
    },
    {
      id: "conns",
      header: "Connections",
      width: "8rem",
      align: "right",
      hideBelow: "md",
      cell: (b) => <span data-tnum>{count(b.connections)}</span>,
    },
  ];

  return (
    <div className="space-y-6">
      <PageHeader
        title={gw?.label ?? "Gateway"}
        meta={gw && <ConnectionPill connected={gw.connected} size="sm" />}
        description={
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {gw && <Identifier value={gw.id} />}
            {gw?.services.map((s) => (
              <Badge key={`${s.name}:${s.port}`} tone="neutral" className="font-mono">
                {s.name}:{s.port}
              </Badge>
            ))}
          </span>
        }
        actions={
          <Segmented<Range>
            label="Period"
            value={range}
            onChange={setRange}
            options={[
              { value: "7d", label: "7 days" },
              { value: "30d", label: "30 days" },
            ]}
          />
        }
      />

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          label="Received from tasks"
          icon={ArrowDownToLine}
          tone="accent"
          loading={!totals}
          value={<span data-tnum>{bytes(totals?.received_from_tasks_bytes)}</span>}
          sub="All time"
        />
        <StatCard
          label="Sent to tasks"
          icon={ArrowUpFromLine}
          tone="success"
          loading={!totals}
          value={<span data-tnum>{bytes(totals?.sent_to_tasks_bytes)}</span>}
          sub="All time"
        />
        <StatCard
          label="Connections"
          icon={Plug}
          loading={!totals}
          value={<span data-tnum>{count(totals?.connections)}</span>}
          sub="Closed connections, all time"
        />
      </div>

      <Card>
        <CardHeader
          title="Traffic over time"
          description={`${bytes(periodTotal.received)} received from tasks and ${bytes(periodTotal.sent)} sent to tasks in the last ${range === "7d" ? "7 days" : "30 days"}, per ${step}.`}
        />
        <CardBody>
          {traffic.loading ? (
            <Skeleton className="h-48 rounded-lg" />
          ) : (
            <StackedChart
              points={points}
              series={SERIES}
              format={bytes}
              formatTime={(at) =>
                step === "day" ? new Date(at).toLocaleDateString(undefined, { month: "short", day: "numeric" }) : shortDateTime(at)
              }
              summary={`Gateway traffic per ${step} over the last ${range}: ${bytes(periodTotal.received)} received from tasks, ${bytes(periodTotal.sent)} sent to tasks.`}
              emptyLabel="No traffic in this period"
            />
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader
          title="Busiest tasks"
          description={`Which tasks moved the most through this gateway in the last ${range === "7d" ? "7 days" : "30 days"}.`}
        />
        <DataTable
          caption="Busiest tasks"
          columns={columns}
          rows={traffic.data?.busiest_tasks ?? []}
          getRowKey={(b) => b.task_id}
          loading={traffic.loading}
          minWidth={520}
          empty={<p className="px-5 py-8 text-center text-sm text-muted">No task used this gateway in this period.</p>}
        />
      </Card>

      <TrafficLegend />
    </div>
  );
}
