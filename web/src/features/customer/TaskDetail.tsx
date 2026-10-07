import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { CircleStop, Network, RotateCcw, Terminal } from "lucide-react";
import { ApiError, apiGet, apiPost, errorMessage } from "@/lib/api";
import { bytes, cpu, duration, memory, money, relativeTime, shortDateTime, dateTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLogStream } from "@/lib/hooks/useLogStream";
import { useNow } from "@/lib/hooks/useNow";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { useLiveEvents, useLiveStatus } from "@/lib/live";
import { costByTask, taskCommand, taskDurationMs } from "@/lib/tasks";
import type { Gateway, LedgerEntry, Task, TaskTraffic } from "@/lib/types";
import { LogViewer } from "@/components/LogViewer";
import { TaskResources } from "./TaskResources";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
import { ConfirmDialog } from "@/ui/Dialog";
import { ErrorState } from "@/ui/EmptyState";
import { Identifier, Truncate } from "@/ui/Identifier";
import { KeyValueList } from "@/ui/KeyValue";
import { NotFound } from "@/ui/NotFound";
import { PageHeader } from "@/ui/PageHeader";
import { Skeleton } from "@/ui/Skeleton";
import { TaskStatusPill } from "@/ui/StatusPill";
import { Timeline, type TimelineStep } from "@/ui/Timeline";
import { useToast } from "@/ui/Toast";
import { exitReason, getTaskStatus, isTerminalState } from "@/ui/status";

function buildTimeline(t: Task, now: number): TimelineStep[] {
  const terminal = isTerminalState(t.state);
  const status = getTaskStatus(t.state, t.exit_reason);
  const waited = t.started_at_ms ? t.started_at_ms - t.created_at_ms : undefined;

  const steps: TimelineStep[] = [
    { id: "submitted", title: "Submitted", time: shortDateTime(t.created_at_ms), tone: "accent", state: "done" },
  ];

  if (t.started_at_ms) {
    steps.push({
      id: "started",
      title: "Started on a node",
      time: shortDateTime(t.started_at_ms),
      detail: waited !== undefined ? `Waited ${duration(waited)} in the queue` : undefined,
      tone: "accent",
      state: "done",
    });
  } else if (!terminal) {
    const starting = t.state === "dispatched" || t.state === "reserved";
    steps.push({
      id: "started",
      title: starting ? "Starting" : "Waiting for a node",
      detail: starting ? "Pulling the image and starting the container." : `Queued for ${duration(now - t.created_at_ms)}.`,
      tone: starting ? "info" : "neutral",
      state: "active",
    });
  } else {
    steps.push({ id: "started", title: "Never started", tone: "neutral", state: "done" });
  }

  if (terminal) {
    const ran = taskDurationMs(t, now);
    steps.push({
      id: "finished",
      title: status.label,
      time: t.finished_at_ms ? shortDateTime(t.finished_at_ms) : undefined,
      detail: ran !== undefined ? `Ran for ${duration(ran)}` : undefined,
      tone: status.tone,
      state: "done",
    });
  } else if (t.cancel_requested) {
    steps.push({
      id: "stopping",
      title: "Stopping",
      detail: "You asked for this to stop. Waiting for the node to confirm.",
      tone: "warning",
      state: "active",
    });
  } else if (t.started_at_ms) {
    steps.push({
      id: "running",
      title: "Running",
      detail: `For ${duration(now - t.started_at_ms)} so far.`,
      tone: "accent",
      state: "active",
    });
  } else {
    steps.push({ id: "finish", title: "Finish", state: "pending" });
  }
  return steps;
}

function Stat({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 bg-surface p-4">
      <p className="text-xs font-medium text-muted">{label}</p>
      <div className="mt-1.5 min-w-0 text-sm font-medium text-fg">{children}</div>
    </div>
  );
}

export function TaskDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const liveStatus = useLiveStatus();

  const task = useAsync((s) => apiGet<Task>(`/api/portal/customer/tasks/${encodeURIComponent(id)}`, s), [id]);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/customer/ledger", s), []);
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);
  const traffic = useAsync((s) => apiGet<TaskTraffic>(`/api/portal/customer/tasks/${encodeURIComponent(id)}/traffic`, s), [id]);
  const logs = useLogStream(task.data ? id : undefined);

  usePageTitle(task.data ? `Task ${id.slice(0, 12)}…` : "Task");

  const t = task.data;
  const active = !!t && !isTerminalState(t.state);
  const now = useNow(1000, active);

  const reloadTask = task.reload;
  const reloadLedger = ledger.reload;
  useLiveEvents((e) => {
    if (e.type === "task_state" && e.task_id === id) {
      task.setData((cur) => (cur ? { ...cur, state: e.state, node_id: e.node_id ?? cur.node_id } : cur));
      reloadTask();
      if (isTerminalState(e.state)) reloadLedger();
    }
  });

  // If live updates drop while the task is active, fall back to polling so
  // the page never freezes on a stale state.
  useEffect(() => {
    if (!active || liveStatus === "live") return;
    const timer = window.setInterval(() => reloadTask(), 4000);
    return () => window.clearInterval(timer);
  }, [active, liveStatus, reloadTask]);

  // Traffic moves while the task runs (gateways report every ~10 s) and settles once it ends.
  const reloadTraffic = traffic.reload;
  useEffect(() => {
    if (!active) {
      reloadTraffic();
      return;
    }
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") reloadTraffic();
    }, 10_000);
    return () => window.clearInterval(timer);
  }, [active, reloadTraffic]);
  const trafficFor = (gatewayId: string, hostname: string) => {
    return traffic.data?.rows.filter((r) => r.gateway_id === gatewayId && r.service === hostname);
  };

  const rerun = useCallback(() => navigate("/tasks/new", { state: { from: t } }), [navigate, t]);

  const toast = useToast();
  const [confirmStop, setConfirmStop] = useState(false);
  const [stopping, setStopping] = useState(false);

  async function stopTask() {
    setStopping(true);
    try {
      const updated = await apiPost<Task>(`/api/portal/customer/tasks/${encodeURIComponent(id)}/cancel`);
      task.setData(() => updated);
      setConfirmStop(false);
      if (updated.cancel_requested)
        toast.toast({ title: "Stopping the task", description: "The node is stopping it now.", tone: "neutral" });
      else toast.success("Task stopped", "It hadn't started, so nothing was charged.");
    } catch (err) {
      setConfirmStop(false);
      if (err instanceof ApiError && err.status === 422) {
        toast.toast({ title: "Already finished", description: "This task finished before it could be stopped.", tone: "neutral" });
        reloadTask();
        reloadLedger();
      } else {
        toast.error("Couldn't stop the task", errorMessage(err));
      }
    } finally {
      setStopping(false);
    }
  }

  if (task.errorStatus === 404) {
    return (
      <NotFound
        title="Task not found"
        description="This task doesn't exist, or it belongs to a different account."
        backTo="/tasks"
        backLabel="Back to tasks"
      />
    );
  }
  if (task.error && !t) return <ErrorState title="Couldn't load this task" message={task.error} onRetry={task.reload} />;
  if (!t) {
    return (
      <div className="space-y-6" aria-busy>
        <Skeleton className="h-9 w-80 max-w-full" />
        <Skeleton className="h-24 w-full rounded-xl" />
        <Skeleton className="h-[30rem] w-full rounded-xl" />
      </div>
    );
  }

  const status = getTaskStatus(t.state, t.exit_reason);
  const reason = exitReason(t.exit_reason, t.exit_code, !!t.started_at_ms);
  const cost = costByTask(ledger.data).get(t.id);
  const ran = taskDurationMs(t, now);
  const cmd = taskCommand(t);
  const gwLabel = (gid: string) => gateways.data?.find((g) => g.id === gid)?.label;
  const env = Object.entries(t.env ?? {});

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <Identifier value={t.id} full copy textClassName="text-xl sm:text-2xl font-semibold tracking-tight" className="max-w-full" />
        }
        meta={<TaskStatusPill state={t.state} stopping={t.cancel_requested} reason={t.exit_reason} />}
        description={
          <>
            Submitted {relativeTime(t.created_at_ms, now)} · <span className="text-subtle">{dateTime(t.created_at_ms)}</span>
          </>
        }
        actions={
          <>
            {active && (
              <Button variant="danger" onClick={() => setConfirmStop(true)} disabled={t.cancel_requested} loading={t.cancel_requested}>
                {!t.cancel_requested && <CircleStop />}
                {t.cancel_requested ? "Stopping…" : "Stop task"}
              </Button>
            )}
            <Button variant="secondary" onClick={rerun}>
              <RotateCcw />
              Run again
            </Button>
          </>
        }
      />

      {reason && isTerminalState(t.state) && reason.tone !== "success" && (
        <Alert tone={reason.tone === "neutral" ? "info" : reason.tone} title={reason.title}>
          {reason.detail}
        </Alert>
      )}

      <Card className="overflow-hidden">
        <div className="grid grid-cols-2 gap-px bg-border lg:grid-cols-4">
          <Stat label="Status">
            <span className="block truncate">{status.label}</span>
            <span className="mt-0.5 block truncate text-xs font-normal text-muted">{status.description.split(".")[0]}</span>
          </Stat>
          <Stat label={active && t.started_at_ms ? "Running for" : "Duration"}>
            <span data-tnum>{duration(ran)}</span>
          </Stat>
          <Stat label="Cost">
            {cost !== undefined ? (
              <span data-tnum>{money(cost)}</span>
            ) : (
              <span className="font-normal text-muted">{active ? "Settles on finish" : "—"}</span>
            )}
          </Stat>
          <Stat label="Node">
            {t.node_id ? <Identifier value={t.node_id} /> : <span className="font-normal text-muted">Not assigned yet</span>}
          </Stat>
        </div>
      </Card>

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="min-w-0 space-y-6">
          <TaskResources task={t} traffic={traffic.data} />
          <section className="min-w-0 space-y-3" aria-labelledby="output-h">
            <div className="flex items-center gap-2">
              <Terminal className="size-4 text-muted" aria-hidden />
              <h2 id="output-h" className="text-sm font-semibold text-fg">
                Output
              </h2>
            </div>
            <LogViewer
              taskId={t.id}
              lines={logs.lines}
              status={logs.status}
              truncated={logs.truncated}
              onRetry={logs.restart}
              taskFinished={isTerminalState(t.state)}
            />
          </section>
        </div>

        <aside className="min-w-0 space-y-6">
          <Card>
            <CardHeader title="Lifecycle" />
            <CardBody>
              <Timeline steps={buildTimeline(t, now)} />
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Resources" />
            <CardBody>
              <KeyValueList
                items={[
                  { label: "CPU", value: <span data-tnum>{cpu(t.cores)}</span> },
                  { label: "Memory", value: <span data-tnum>{memory(t.memory_mb)}</span> },
                  { label: "Disk", value: <span data-tnum>{memory(t.disk_mb)}</span>, hidden: !t.disk_mb },
                  {
                    label: "Time limit",
                    value: <span data-tnum>{duration((t.wall_timeout_s ?? 0) * 1000)}</span>,
                    hidden: !t.wall_timeout_s,
                  },
                  { label: "Attempt", value: <span data-tnum>{(t.attempt ?? 0) + 1}</span>, hidden: !t.attempt },
                ]}
              />
            </CardBody>
          </Card>

          {t.tunnel_targets && t.tunnel_targets.length > 0 && (
            <Card>
              <CardHeader title="Network" description="This task can reach only these gateways." />
              {traffic.data &&
                traffic.data.totals.connections + traffic.data.totals.received_from_tasks_bytes + traffic.data.totals.sent_to_tasks_bytes >
                  0 && (
                  <p className="px-5 pb-1 text-xs text-muted">
                    In total this task sent{" "}
                    <span className="font-medium text-fg" data-tnum>
                      {bytes(traffic.data.totals.received_from_tasks_bytes)}
                    </span>{" "}
                    to your services and received{" "}
                    <span className="font-medium text-fg" data-tnum>
                      {bytes(traffic.data.totals.sent_to_tasks_bytes)}
                    </span>{" "}
                    back.
                  </p>
                )}
              <CardBody className="space-y-3">
                {t.tunnel_targets.map((g) => (
                  <div
                    key={`${g.gateway_id}:${g.hostname}`}
                    className="flex min-w-0 items-start gap-3 rounded-lg border border-border bg-raised/40 p-3"
                  >
                    <Network className="mt-0.5 size-4 shrink-0 text-accent" aria-hidden />
                    <div className="min-w-0 flex-1 space-y-0.5">
                      <p className="truncate font-mono text-[0.8125rem] text-fg">
                        {g.hostname}:{g.port}
                      </p>
                      <p className="truncate text-xs text-muted">
                        via{" "}
                        <Link to="/gateways" className="text-accent hover:underline">
                          {gwLabel(g.gateway_id) ?? "gateway"}
                        </Link>
                      </p>
                      <Identifier value={g.gateway_id} />
                      <TargetTraffic rows={trafficFor(g.gateway_id, g.hostname)} />
                    </div>
                  </div>
                ))}
              </CardBody>
            </Card>
          )}

          <Card>
            <CardHeader title="Definition" />
            <CardBody className="space-y-4">
              <div className="min-w-0">
                <p className="mb-1.5 text-xs font-medium text-muted">Image</p>
                <CodeBlock code={t.image} wrap copyLabel="Copy image" />
              </div>
              <div className="min-w-0">
                <p className="mb-1.5 text-xs font-medium text-muted">Command</p>
                {cmd ? <CodeBlock code={cmd} wrap /> : <p className="text-sm text-muted">The image's default command.</p>}
              </div>
              {env.length > 0 && (
                <div className="min-w-0">
                  <p className="mb-1.5 text-xs font-medium text-muted">Environment</p>
                  <KeyValueList
                    items={env.map(([k, v]) => ({
                      label: <Truncate className="font-mono text-xs">{k}</Truncate>,
                      value: <span className="font-mono text-xs">{v}</span>,
                    }))}
                  />
                </div>
              )}
            </CardBody>
          </Card>
        </aside>
      </div>

      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title="Stop this task?"
        description={
          t.started_at_ms
            ? "It will be stopped now and can't be resumed. You'll be charged only for the time it has already run."
            : "It hasn't started yet, so nothing will be charged."
        }
        confirmLabel="Stop task"
        danger
        busy={stopping}
        onConfirm={stopTask}
      />
    </div>
  );
}

function TargetTraffic({ rows }: { rows: TaskTraffic["rows"] | undefined }) {
  if (!rows || rows.length === 0) return null;
  const sent = rows.reduce((n, r) => n + r.received_from_tasks_bytes, 0);
  const got = rows.reduce((n, r) => n + r.sent_to_tasks_bytes, 0);
  return (
    <p className="truncate pt-1 text-xs text-muted" data-tnum>
      ↑ {bytes(sent)} sent · ↓ {bytes(got)} received
    </p>
  );
}
