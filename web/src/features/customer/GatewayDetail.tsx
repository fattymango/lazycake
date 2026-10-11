import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ArrowDownToLine, ArrowUpFromLine, Plug } from "lucide-react";
import { apiGet } from "@/lib/api";
import { bytes, count, dateTime, relativeTime, shortDateTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useInterval } from "@/lib/hooks/useInterval";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { gatewayActivity, lastTrafficAtMs } from "@/lib/gatewayActivity";
import { bandwidthLimit } from "@/lib/gatewayLimit";
import { fillBuckets, type StackPoint } from "@/lib/series";
import type { Gateway, GatewayTraffic, Task } from "@/lib/types";
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
import { KeyValueList } from "@/ui/KeyValue";
import { ConnectionPill, TaskStatusPill } from "@/ui/StatusPill";
import { UpdateNotice } from "@/ui/UpdateNotice";
import { LineChart, type ChartSeries } from "@/ui/LineChart";
import { Split, StatRail } from "@/ui/Split";
import { TrafficLegend } from "./TrafficLegend";
import { DeleteGatewayDialog } from "./DeleteGatewayDialog";
import { DeleteGatewayButton } from "./Gateways";

type Range = "7d" | "30d";
const WINDOW: Record<Range, number> = { "7d": 7 * 86_400_000, "30d": 30 * 86_400_000 };

const SERIES: ChartSeries[] = [
  { key: "received", label: "Received from tasks", color: "rgb(var(--accent))" },
  { key: "sent", label: "Sent to tasks", color: "rgb(var(--success))" },
];

type Busiest = GatewayTraffic["busiest_tasks"][number];

export function GatewayDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const [range, setRange] = useState<Range>("7d");
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);
  const traffic = useAsync(
    (s) => apiGet<GatewayTraffic>(`/api/portal/customer/gateways/${encodeURIComponent(id)}/traffic?range=${range}`, s),
    [id, range]
  );
  const tasks = useAsync((s) => apiGet<Task[]>("/api/portal/customer/tasks", s), []);
  useInterval(() => {
    // Once deleted the gateway disappears from the list: don't let a refresh swap this page for "not found" under the open dialog.
    if (deleting) return;
    tasks.reload();
    gateways.reload();
    traffic.reload();
  }, 30_000);

  const activity = useMemo(() => gatewayActivity(tasks.data ?? [], id), [tasks.data, id]);
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

  const active = [...activity.running, ...activity.queued];
  const lastTraffic = lastTrafficAtMs(traffic.data?.series);
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
          <div className="flex items-center gap-2">
            {gw && <DeleteGatewayButton gatewayId={gw.id} label={gw.label} tasks={tasks.data} onClick={() => setDeleting(true)} />}
            <Segmented<Range>
              label="Period"
              value={range}
              onChange={setRange}
              options={[
                { value: "7d", label: "7 days" },
                { value: "30d", label: "30 days" },
              ]}
            />
          </div>
        }
      />

      <Split
        main={
          <>
            <Card>
              <CardHeader
                title="Traffic over time"
                description={`${bytes(periodTotal.received)} received from tasks and ${bytes(periodTotal.sent)} sent to tasks in the last ${range === "7d" ? "7 days" : "30 days"}, per ${step}.`}
              />
              <CardBody>
                {traffic.loading ? (
                  <Skeleton className="h-48 rounded-lg" />
                ) : (
                  <LineChart
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
          </>
        }
        side={
          <>
            <StatRail>
              <StatCard
                compact
                className="col-span-2"
                label="Received from tasks"
                icon={ArrowDownToLine}
                tone="accent"
                loading={!totals}
                value={<span data-tnum>{bytes(totals?.received_from_tasks_bytes)}</span>}
                sub="All time"
              />
              <StatCard
                compact
                label="Sent to tasks"
                icon={ArrowUpFromLine}
                tone="success"
                loading={!totals}
                value={<span data-tnum>{bytes(totals?.sent_to_tasks_bytes)}</span>}
                sub="All time"
              />
              <StatCard
                compact
                label="Connections"
                icon={Plug}
                loading={!totals}
                value={<span data-tnum>{count(totals?.connections)}</span>}
                sub="Closed connections, all time"
              />
            </StatRail>
            {gw && (
              <Card>
                <CardHeader
                  title="Targets and activity"
                  description="What this gateway lets tasks reach, and what is using it right now."
                />
                <CardBody className="space-y-6">
                  <div className="min-w-0 space-y-3">
                    <p className="text-xs font-medium text-muted">Targets</p>
                    {gw.services.length === 0 ? (
                      <p className="text-sm text-subtle">No services published.</p>
                    ) : (
                      <ul className="space-y-2">
                        {gw.services.map((svc) => (
                          <li key={`${svc.name}:${svc.port}`} className="rounded-lg border border-border bg-raised/40 px-3 py-2 text-sm">
                            <span className="font-mono font-semibold text-fg">{svc.name}</span>
                            <span className="text-muted"> → port </span>
                            <span className="font-mono text-fg">{svc.port}</span>
                            <span className="text-muted"> on the gateway's machine</span>
                          </li>
                        ))}
                      </ul>
                    )}
                    <p className="text-xs leading-5 text-muted">A task reaches a target by its name, and nothing else on that machine.</p>
                  </div>
                  <KeyValueList
                    items={[
                      { label: "Status", value: <ConnectionPill connected={gw.connected} size="sm" /> },
                      {
                        label: "Version",
                        value: (
                          <span className="flex flex-wrap items-center gap-2">
                            <span className="font-mono">{gw.version || "unknown"}</span>
                            <UpdateNotice kind="gateway" info={gw} name={gw.label} />
                          </span>
                        ),
                      },
                      { label: "Created", value: dateTime(gw.created_at_ms) },
                      {
                        label: "Bandwidth limit",
                        value: bandwidthLimit(gw.max_mbps).reported ? (
                          <span data-tnum>{bandwidthLimit(gw.max_mbps).text}</span>
                        ) : (
                          <span className="text-muted" title="This gateway is older than limit reporting. Update it and it will say.">
                            {bandwidthLimit(gw.max_mbps).text}
                          </span>
                        ),
                      },
                      {
                        label: "Last traffic",
                        value: traffic.loading ? (
                          "…"
                        ) : lastTraffic ? (
                          <span title={dateTime(lastTraffic)}>{relativeTime(lastTraffic)}</span>
                        ) : (
                          `None in the last ${range === "7d" ? "7" : "30"} days`
                        ),
                      },
                      {
                        label: "Last task",
                        value: tasks.loading ? (
                          "…"
                        ) : activity.lastTask ? (
                          <span className="flex flex-wrap items-center gap-2">
                            <Link to={`/tasks/${activity.lastTask.id}`} className="text-accent hover:underline">
                              <Identifier value={activity.lastTask.id} />
                            </Link>
                            <span className="text-muted">{relativeTime(activity.lastTaskAtMs)}</span>
                          </span>
                        ) : (
                          "None among your recent tasks"
                        ),
                      },
                      { label: "Running tasks", value: <span data-tnum>{tasks.loading ? "…" : count(activity.running.length)}</span> },
                      { label: "Queued tasks", value: <span data-tnum>{tasks.loading ? "…" : count(activity.queued.length)}</span> },
                    ]}
                  />
                </CardBody>
                {active.length > 0 && (
                  <ul className="divide-y divide-border border-t border-border">
                    {active.map((t) => (
                      <li key={t.id} className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
                        <Link to={`/tasks/${t.id}`} className="min-w-0 text-accent hover:underline">
                          <Identifier value={t.id} />
                        </Link>
                        <TaskStatusPill state={t.state} stopping={t.cancel_requested} size="sm" />
                      </li>
                    ))}
                  </ul>
                )}
              </Card>
            )}
          </>
        }
      />

      <TrafficLegend />
      <DeleteGatewayDialog gateway={deleting && gw ? gw : null} onClose={() => setDeleting(false)} onClosed={() => navigate("/gateways")} />
    </div>
  );
}
