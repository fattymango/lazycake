import { useEffect, useMemo, useState } from "react";
import { Gauge } from "lucide-react";
import { apiGet } from "@/lib/api";
import { bytes, cores, coresLabel, memory } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import type { StackPoint } from "@/lib/series";
import { bucketReadings, bucketTraffic, taskStack, type TaskMetric } from "@/lib/taskusage";
import type { Task, TaskTraffic, TaskUsage } from "@/lib/types";
import { isTerminalState } from "@/ui/status";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { ErrorState } from "@/ui/EmptyState";
import { Segmented } from "@/ui/Segmented";
import { Skeleton } from "@/ui/Skeleton";
import { LineChart, type ChartSeries } from "@/ui/LineChart";

type Tab = TaskMetric | "gateway";

const clock = (at: number) => new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const clockShort = (at: number) => new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

/**
 * What the task actually used while it ran, against the limits it asked for: CPU, memory and
 * tunnel traffic from the machine's agent, and what the customer's own gateway counted. The
 * agent's numbers are reported by the machine it ran on, so this is for information only.
 */
export function TaskResources({ task, traffic }: { task: Task; traffic: TaskTraffic | undefined }) {
  const [tab, setTab] = useState<Tab>("cpu");
  const usage = useAsync((s) => apiGet<TaskUsage>(`/api/portal/customer/tasks/${encodeURIComponent(task.id)}/usage`, s), [task.id]);
  const active = !isTerminalState(task.state);

  // Fresh readings arrive about every 15 s while the task runs; once it ends, read them one last time.
  const reload = usage.reload;
  useEffect(() => {
    if (!active) {
      reload();
      return;
    }
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") reload();
    }, 15_000);
    return () => window.clearInterval(timer);
  }, [active, reload]);

  const u = usage.data;
  const start = task.started_at_ms ?? u?.points[0]?.at_ms ?? task.created_at_ms;
  const end = task.finished_at_ms ?? Date.now();
  const hasTunnel = (task.tunnel_targets?.length ?? 0) > 0;

  const points = useMemo<StackPoint[]>(() => {
    if (!u) return [];
    if (tab === "gateway") {
      const raw = (traffic?.series ?? []).map((p) => ({
        at_ms: p.at_ms,
        received: p.received_from_tasks_bytes,
        sent: p.sent_to_tasks_bytes,
      }));
      return bucketTraffic(raw, start, end);
    }
    // From the first reading to the last: the first one arrives a few seconds after the task starts,
    // and that lead-in is not a gap in the data.
    const first = u.points[0]?.at_ms ?? start;
    const last = u.points[u.points.length - 1]?.at_ms ?? end;
    return taskStack(bucketReadings(u.points, first, last), tab);
  }, [u, traffic, tab, start, end]);

  const series: ChartSeries[] =
    tab === "cpu"
      ? [{ key: "cpu", label: "CPU used", color: "rgb(var(--accent))" }]
      : tab === "memory"
        ? [{ key: "memory", label: "Memory used", color: "rgb(var(--success))" }]
        : tab === "network"
          ? [
              { key: "out", label: "Sent by the task", color: "rgb(var(--accent))" },
              { key: "in", label: "Received by the task", color: "rgb(var(--success))" },
            ]
          : [
              { key: "received", label: "Received from tasks", color: "rgb(var(--accent))" },
              { key: "sent", label: "Sent to tasks", color: "rgb(var(--success))" },
            ];

  // Nothing to show before the task has started, and no point in an empty card for one that
  // finished without any reading when the machine's agent doesn't report usage.
  const started = !!task.started_at_ms;
  if (!started && !u?.supported) return null;
  if (usage.error && !u) {
    return (
      <Card>
        <ErrorState title="Couldn't load resource use" message={usage.error} onRetry={usage.reload} compact />
      </Card>
    );
  }

  const format = tab === "cpu" ? coresLabel : bytes;
  const limit =
    tab === "cpu" && u
      ? { value: u.limits.cores, label: `${u.limits.cores} cores requested` }
      : tab === "memory" && u
        ? { value: u.limits.memory_mb * 1024 * 1024, label: `${memory(u.limits.memory_mb)} requested` }
        : undefined;
  const tabs = [
    { value: "cpu" as Tab, label: "CPU" },
    { value: "memory" as Tab, label: "Memory" },
    ...(hasTunnel
      ? [
          { value: "network" as Tab, label: "Network" },
          { value: "gateway" as Tab, label: "Gateway" },
        ]
      : []),
  ];
  const peakCPU = u ? Math.max(0, ...u.points.map((p) => p.cpu_cores)) : 0;
  const peakMem = u?.summary?.peak_memory_bytes ?? 0;

  return (
    <section aria-labelledby="resources-h" className="min-w-0 space-y-3">
      <div className="flex items-center gap-2">
        <Gauge className="size-4 text-muted" aria-hidden />
        <h2 id="resources-h" className="text-sm font-semibold text-fg">
          Resource use
        </h2>
      </div>
      <Card>
        <CardHeader
          title={
            u?.supported
              ? `Peak ${cores(peakCPU)} of ${u.limits.cores} cores · ${bytes(peakMem)} of ${memory(u.limits.memory_mb)} memory`
              : u && !u.agent_reports_usage
                ? "Usage isn't available for this machine yet"
                : "Waiting for readings"
          }
          description="As reported by the machine the task ran on, about every 15 seconds."
          action={<Segmented<Tab> label="What to chart" value={tab} onChange={setTab} options={tabs} />}
        />
        <CardBody>
          {!u ? (
            <Skeleton className="h-48 rounded-lg" />
          ) : !u.supported ? (
            <p className="mx-auto max-w-lg py-10 text-center text-sm leading-6 text-muted">
              {!u.agent_reports_usage
                ? "The machine running this task has an older agent that can't report usage yet. Once its owner updates the agent, tasks on it will show their CPU, memory and network here."
                : active
                  ? "Waiting for the first reading from the machine (it arrives within about 15 seconds)."
                  : "The machine didn't report usage for this task."}
            </p>
          ) : (
            <LineChart
              points={points}
              series={series}
              format={format}
              axisFormat={tab === "cpu" ? cores : undefined}
              axisUnit={tab === "cpu" ? "cores" : undefined}
              formatTime={clock}
              axisLabel={clockShort}
              limit={limit}
              gapLabel="No reading received"
              showZero={tab !== "gateway"}
              emptyLabel={tab === "gateway" ? "No gateway traffic yet" : tab === "network" ? "No tunnel traffic" : "No readings"}
              summary={`Task ${tab === "gateway" ? "gateway traffic" : tab === "network" ? "tunnel traffic" : tab} over the time it ran.`}
            />
          )}
        </CardBody>
      </Card>
    </section>
  );
}
