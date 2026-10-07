import { useMemo, useState } from "react";
import { Activity, Cpu, HardDrive, MemoryStick } from "lucide-react";
import { apiGet } from "@/lib/api";
import { bytes, cores, memory, relativeTime, shortDateTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useInterval } from "@/lib/hooks/useInterval";
import { stackTotal } from "@/lib/series";
import type { Node, NodeUsage } from "@/lib/types";
import { OTHERS_KEY, bandClass, percent, usageAt, usageStack, type UsageMetric } from "@/lib/usage";
import { Alert } from "@/ui/Alert";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { ErrorState } from "@/ui/EmptyState";
import { ProgressBar } from "@/ui/ProgressBar";
import { Segmented } from "@/ui/Segmented";
import { Skeleton } from "@/ui/Skeleton";
import { StackedChart, type ChartSeries } from "@/ui/StackedChart";
import type { Tone } from "@/ui/tone";

type Range = "24h" | "7d";

const GAUGE_STALE_MS = 90_000;

function Gauge({
  icon: Icon,
  label,
  value,
  detail,
  fraction,
  tone,
}: {
  icon: typeof Cpu;
  label: string;
  value: string;
  detail: string;
  fraction: number;
  tone: Tone;
}) {
  return (
    <li className="min-w-0 space-y-2">
      <div className="flex items-center justify-between gap-3">
        <span className="flex items-center gap-2 text-xs font-medium text-muted">
          <Icon className="size-3.5" aria-hidden />
          {label}
        </span>
        <span className="text-sm font-semibold text-fg" data-tnum>
          {value}
        </span>
      </div>
      <ProgressBar value={fraction} tone={tone} label={label} />
      <p className="truncate text-xs text-muted" data-tnum>
        {detail}
      </p>
    </li>
  );
}

function toneFor(fraction: number): Tone {
  return fraction >= 0.9 ? "danger" : fraction >= 0.7 ? "warning" : "accent";
}

/**
 * What the machine and its tasks are using: live gauges, then a stacked history where
 * hovering a moment lists who was using what. Reported by the machine's own agent, so it is
 * for the provider's eyes only and never affects billing or trust.
 */
export function MachineUsage({ node, taskLabel }: { node: Node; taskLabel: (id: string) => string }) {
  const [range, setRange] = useState<Range>("24h");
  const [metric, setMetric] = useState<UsageMetric>("cpu");
  const usage = useAsync(
    (s) => apiGet<NodeUsage>(`/api/portal/provider/nodes/${encodeURIComponent(node.id)}/usage?range=${range}`, s),
    [node.id, range]
  );
  useInterval(() => usage.reload(), 15_000);

  const u = usage.data;
  const points = useMemo(() => (u ? usageStack(u, metric) : []), [u, metric]);
  const series = useMemo<ChartSeries[]>(() => {
    if (!u) return [];
    if (metric === "disk") return [{ key: "disk", label: "Disk used", className: "bg-info" }];
    // On the network chart a task's band is its traffic in both directions.
    const named = u.tasks.map((id, i) => ({ key: id, label: taskLabel(id), className: bandClass(i) }));
    const hasOthers = u.series.some((p) => p.per_task[OTHERS_KEY]);
    return hasOthers ? [...named, { key: OTHERS_KEY, label: "Other tasks", className: "bg-muted/60" }] : named;
  }, [u, metric, taskLabel]);

  if (usage.error && !u) {
    return (
      <Card>
        <ErrorState title="Couldn't load usage" message={usage.error} onRetry={usage.reload} compact />
      </Card>
    );
  }
  if (!u) return <Skeleton className="h-80 w-full rounded-xl" aria-busy />;

  if (!u.supported) {
    return (
      <Alert tone="info" title="Usage isn't available for this machine yet">
        {node.connected
          ? "Its agent hasn't reported any. Update the agent to the latest version and usage will appear here within a minute."
          : "It hasn't reported any usage yet. Once the agent is running a recent version, it will appear here."}
      </Alert>
    );
  }

  const l = u.latest;
  const fresh = !!l && Date.now() - l.at_ms < GAUGE_STALE_MS;
  const offeredMem = node.offer_memory_mb * 1024 * 1024;
  const offeredDisk = node.offer_disk_mb * 1024 * 1024;
  const periodLabel = range === "24h" ? "last 24 hours" : "last 7 days";

  const limit =
    metric === "cpu"
      ? { value: node.offer_cores, label: `${node.offer_cores} cores offered` }
      : metric === "memory"
        ? { value: offeredMem, label: `${memory(node.offer_memory_mb)} offered` }
        : undefined;
  const format = metric === "cpu" ? (v: number) => `${cores(v)} cores` : bytes;
  const metricName = { cpu: "CPU", memory: "Memory", disk: "Disk", network: "Tunnel traffic" }[metric];

  return (
    <Card>
      <CardHeader
        title="Usage"
        description={
          l ? (
            <span>
              {fresh ? "Live" : "Last reading"} · {relativeTime(l.at_ms)}. Reported by this machine's agent; it doesn't affect billing or
              trust.
            </span>
          ) : (
            "Reported by this machine's agent."
          )
        }
      />
      <CardBody className="space-y-6">
        {l && (
          <ul className="grid gap-5 sm:grid-cols-3" aria-label="Current usage">
            <Gauge
              icon={Cpu}
              label="CPU used by tasks"
              value={fresh ? `${cores(l.tasks_cpu_cores)} of ${node.offer_cores}` : "—"}
              detail={fresh ? `Whole machine ${percent(l.host_cpu_busy)} busy across ${l.host_cpu_count} cores` : "Not reporting right now"}
              fraction={fresh && node.offer_cores > 0 ? l.tasks_cpu_cores / node.offer_cores : 0}
              tone={toneFor(node.offer_cores > 0 ? l.tasks_cpu_cores / node.offer_cores : 0)}
            />
            <Gauge
              icon={MemoryStick}
              label="Memory used by tasks"
              value={fresh ? bytes(l.tasks_memory_bytes) : "—"}
              detail={
                fresh
                  ? `of ${memory(node.offer_memory_mb)} offered · machine ${bytes(l.host_mem_used_bytes)} of ${bytes(l.host_mem_total_bytes)}`
                  : "Not reporting right now"
              }
              fraction={fresh && offeredMem > 0 ? l.tasks_memory_bytes / offeredMem : 0}
              tone={toneFor(offeredMem > 0 ? l.tasks_memory_bytes / offeredMem : 0)}
            />
            <Gauge
              icon={HardDrive}
              label="Disk"
              value={l.disk_total_bytes > 0 ? percent(l.disk_used_bytes / l.disk_total_bytes) : "—"}
              detail={
                l.disk_total_bytes > 0
                  ? `${bytes(l.disk_used_bytes)} of ${bytes(l.disk_total_bytes)} on the agent's disk${offeredDisk > 0 ? ` · ${memory(node.offer_disk_mb)} offered` : ""}`
                  : "Not reported"
              }
              fraction={l.disk_total_bytes > 0 ? l.disk_used_bytes / l.disk_total_bytes : 0}
              tone={toneFor(l.disk_total_bytes > 0 ? l.disk_used_bytes / l.disk_total_bytes : 0)}
            />
          </ul>
        )}

        <div className="space-y-4 border-t border-border pt-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-fg">
              <Activity className="size-4 text-muted" aria-hidden />
              History
            </h3>
            <div className="flex flex-wrap items-center gap-2">
              <Segmented<UsageMetric>
                label="What to chart"
                value={metric}
                onChange={setMetric}
                options={[
                  { value: "cpu", label: "CPU" },
                  { value: "memory", label: "Memory" },
                  { value: "disk", label: "Disk" },
                  { value: "network", label: "Network" },
                ]}
              />
              <Segmented<Range>
                label="Period"
                value={range}
                onChange={setRange}
                options={[
                  { value: "24h", label: "24 hours" },
                  { value: "7d", label: "7 days" },
                ]}
              />
            </div>
          </div>
          <StackedChart
            points={points}
            series={series}
            format={format}
            formatTime={(at) => shortDateTime(at)}
            axisLabel={(at) =>
              range === "24h"
                ? new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                : new Date(at).toLocaleDateString(undefined, { month: "short", day: "numeric" })
            }
            summary={`${metricName} ${metric === "network" ? "moved by" : "used by"} this machine's tasks over the ${periodLabel}, per ${u.step === "hour" ? "hour" : "five minutes"}.`}
            limit={limit}
            emptyLabel={
              metric === "disk"
                ? "No reading in this period"
                : metric === "network"
                  ? "No tunnel traffic in this period"
                  : "No task ran in this period"
            }
            footer={(p) => {
              const pt = usageAt(u, p.at);
              if (!pt) return null;
              if (metric === "cpu")
                return `Tasks ${cores(pt.tasks_cpu_cores)} of ${node.offer_cores} cores · machine ${percent(pt.host_cpu_busy)} busy`;
              if (metric === "network")
                return `Out of the tasks ${bytes(pt.tunnel_out_bytes)} · into the tasks ${bytes(pt.tunnel_in_bytes)}`;
              if (metric === "memory")
                return `Tasks ${bytes(pt.tasks_memory_bytes)} of ${memory(node.offer_memory_mb)} · machine ${bytes(pt.host_mem_used_bytes)}`;
              return `${bytes(pt.disk_used_bytes)} used${stackTotal(p) > 0 && offeredDisk > 0 ? ` · ${memory(node.offer_disk_mb)} offered` : ""}`;
            }}
          />
        </div>
      </CardBody>
    </Card>
  );
}
