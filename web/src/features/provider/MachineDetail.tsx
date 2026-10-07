import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Cpu, Gauge, HardDrive, MemoryStick, Server, Trash2 } from "lucide-react";
import { apiDelete, apiGet, errorMessage } from "@/lib/api";
import { bytes, cpu, dateTime, duration, memory, relativeTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import type { Node, NodeTask } from "@/lib/types";
import { TaskTable } from "@/components/TaskTable";
import { TrustMeter } from "@/components/TrustMeter";
import { MachineUsage } from "./MachineUsage";
import { Alert } from "@/ui/Alert";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { ConfirmDialog } from "@/ui/Dialog";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier, Truncate } from "@/ui/Identifier";
import { NotFound } from "@/ui/NotFound";
import { PageHeader } from "@/ui/PageHeader";
import { Skeleton } from "@/ui/Skeleton";
import { ConnectionPill } from "@/ui/StatusPill";
import { useToast } from "@/ui/Toast";
import { Layers } from "lucide-react";

function Stat({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 bg-surface p-4">
      <p className="text-xs font-medium text-muted">{label}</p>
      <div className="mt-1.5 min-w-0 text-sm font-medium text-fg">{children}</div>
    </div>
  );
}

export function MachineDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const toast = useToast();
  const node = useAsync((s) => apiGet<Node>(`/api/portal/provider/nodes/${encodeURIComponent(id)}`, s), [id]);
  const tasks = useAsync((s) => apiGet<NodeTask[]>(`/api/portal/provider/nodes/${encodeURIComponent(id)}/tasks`, s), [id]);
  const [confirm, setConfirm] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  usePageTitle(node.data?.hostname ?? "Machine");
  useLiveReload(
    () => {
      node.reload();
      tasks.reload();
    },
    (e) => e.type === "node_connected" || e.type === "node_disconnected" || (e.type === "task_state" && e.node_id === id)
  );

  async function remove() {
    setRemoving(true);
    setRemoveError(null);
    try {
      await apiDelete(`/api/portal/provider/nodes/${encodeURIComponent(id)}`);
      toast.success("Machine removed", node.data?.hostname);
      navigate("/", { replace: true });
    } catch (err) {
      setRemoveError(errorMessage(err, "Couldn't remove the machine"));
      setRemoving(false);
      setConfirm(false);
    }
  }

  if (node.errorStatus === 404)
    return <NotFound title="Machine not found" description="This machine doesn't exist, or it was removed." backLabel="Back to overview" />;
  if (node.error && !node.data) return <ErrorState title="Couldn't load this machine" message={node.error} onRetry={node.reload} />;
  if (!node.data) {
    return (
      <div className="space-y-6" aria-busy>
        <Skeleton className="h-9 w-72 max-w-full" />
        <Skeleton className="h-24 w-full rounded-xl" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  }

  const n = node.data;
  const taskLabel = (taskId: string) => `${taskId.slice(0, 8)}…${taskId.slice(-4)}`;
  const usageOf = new Map((tasks.data ?? []).map((t) => [t.id, t.usage] as const));
  return (
    <div className="space-y-6">
      <PageHeader
        title={<Truncate className="max-w-[calc(100vw-4rem)] sm:max-w-xl">{n.hostname}</Truncate>}
        meta={<ConnectionPill connected={n.connected} />}
        description={
          <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
            <Identifier value={n.id} full />
            <Badge tone="neutral" size="sm" className="font-mono">
              {n.arch}
            </Badge>
          </span>
        }
        actions={
          <Button variant="danger" onClick={() => setConfirm(true)}>
            <Trash2 />
            Remove machine
          </Button>
        }
      />

      {removeError && (
        <Alert tone="danger" title="Couldn't remove this machine">
          {removeError}
        </Alert>
      )}
      {!n.connected && (
        <Alert tone="warning" title="This machine is offline">
          It hasn't sent a heartbeat {n.last_heartbeat_at_ms ? relativeTime(n.last_heartbeat_at_ms) : "yet"}. Check that the agent is
          running on it; no tasks are scheduled here until it reconnects.
        </Alert>
      )}

      <Card className="overflow-hidden">
        <div className="grid grid-cols-2 gap-px bg-border lg:grid-cols-4">
          <Stat label="Status">{n.connected ? "Online" : "Offline"}</Stat>
          <Stat label="Trust score">
            <TrustMeter score={n.trust_score} />
          </Stat>
          <Stat label="Last heartbeat">
            <span data-tnum>{relativeTime(n.last_heartbeat_at_ms)}</span>
          </Stat>
          <Stat label="Registered">
            <span data-tnum>{dateTime(n.created_at_ms)}</span>
          </Stat>
        </div>
      </Card>

      <MachineUsage node={n} taskLabel={taskLabel} />

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Card className="min-w-0 overflow-hidden">
          <CardHeader title="Recent tasks" description="Work customers have run on this machine." className="pb-4" />
          <div className="border-t border-border">
            {tasks.error && !tasks.data ? (
              <ErrorState title="Couldn't load tasks" message={tasks.error} onRetry={tasks.reload} compact />
            ) : (
              <TaskTable
                tasks={tasks.data ?? []}
                loading={tasks.loading}
                showNode={false}
                getHref={null}
                rowHover={(t) => {
                  const u = usageOf.get(t.id);
                  if (!u) return <span className="text-muted">No usage reported for this task.</span>;
                  return (
                    <dl className="grid grid-cols-[auto_auto] gap-x-4 gap-y-0.5" data-tnum>
                      <dt className="col-span-2 mb-0.5 font-medium text-fg">What this task used</dt>
                      <dt className="text-muted">CPU time</dt>
                      <dd className="text-right">{duration(u.core_seconds * 1000)} of one core</dd>
                      <dt className="text-muted">Peak memory</dt>
                      <dd className="text-right">{bytes(u.peak_memory_bytes)}</dd>
                      <dt className="text-muted">Tunnel traffic</dt>
                      <dd className="text-right">
                        {bytes(u.tunnel_bytes_to_gateway)} out · {bytes(u.tunnel_bytes_to_task)} in
                      </dd>
                    </dl>
                  );
                }}
                empty={
                  <EmptyState
                    icon={Layers}
                    title="No tasks yet"
                    description="Tasks appear here as soon as this machine picks one up."
                    compact
                  />
                }
              />
            )}
          </div>
        </Card>

        <Card>
          <CardHeader title="Capacity on offer" description="What this machine contributes to the pool." />
          <CardBody className="space-y-5">
            <ul className="space-y-4">
              {[
                { icon: Cpu, label: "CPU", value: cpu(n.offer_cores) },
                { icon: MemoryStick, label: "Memory", value: memory(n.offer_memory_mb) },
                { icon: HardDrive, label: "Disk", value: memory(n.offer_disk_mb) },
                { icon: Gauge, label: "Network", value: n.offer_network_mbps > 0 ? `${n.offer_network_mbps} Mbps` : "No limit set" },
                { icon: Server, label: "Architecture", value: <span className="font-mono text-[0.8125rem]">{n.arch}</span> },
              ].map((r) => (
                <li key={r.label} className="flex items-center justify-between gap-3">
                  <span className="flex items-center gap-2.5 text-sm text-muted">
                    <r.icon className="size-4" aria-hidden />
                    {r.label}
                  </span>
                  <span className="text-sm font-medium text-fg" data-tnum>
                    {r.value}
                  </span>
                </li>
              ))}
            </ul>
            <p className="text-xs leading-5 text-muted">Change what's on offer by restarting the agent with different offer settings.</p>
          </CardBody>
        </Card>
      </div>

      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Remove this machine?"
        description="It will stop receiving tasks and disappear from your fleet. Your earnings history is kept. The agent can register again later with a new token."
        confirmLabel="Remove machine"
        danger
        busy={removing}
        onConfirm={remove}
      />
    </div>
  );
}
