import { useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router-dom";
import { ArrowDownToLine, ArrowUpFromLine, Network, Plus, Trash2 } from "lucide-react";
import { apiGet, apiPost, errorMessage } from "@/lib/api";
import { gatewayActivity } from "@/lib/gatewayActivity";
import { bytes, relativeTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useInterval } from "@/lib/hooks/useInterval";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import type { CreateGatewayResponse, Gateway, Task } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardFooter } from "@/ui/Card";
import { Dialog, DialogContent } from "@/ui/Dialog";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier } from "@/ui/Identifier";
import { Field, Input } from "@/ui/Input";
import { Segmented } from "@/ui/Segmented";
import { PageHeader } from "@/ui/PageHeader";
import { GatewayTester } from "./GatewayTest";
import { DeleteGatewayDialog } from "./DeleteGatewayDialog";
import { GatewayInstallSteps } from "./GatewayInstallSteps";
import { GatewaysOverview } from "./GatewaysOverview";
import { Skeleton } from "@/ui/Skeleton";
import { ConnectionPill } from "@/ui/StatusPill";
import { UpdateNotice } from "@/ui/UpdateNotice";
import { useToast } from "@/ui/Toast";

interface ServiceRow {
  name: string;
  port: string;
}

function CreateGatewayDialog({
  open,
  onOpenChange,
  onCreated,
  connectedIds,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onCreated: () => void;
  connectedIds: Set<string>;
}) {
  const toast = useToast();
  const [label, setLabel] = useState("");
  const [rows, setRows] = useState<ServiceRow[]>([{ name: "", port: "" }]);
  const [mbps, setMbps] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreateGatewayResponse | null>(null);

  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      // reset after the close animation
      window.setTimeout(() => {
        setLabel("");
        setRows([{ name: "", port: "" }]);
        setMbps("");
        setError(null);
        setCreated(null);
      }, 200);
    }
  };

  const valid = label.trim() !== "" && rows.some((r) => r.name.trim() && Number(r.port) > 0);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const services = rows.filter((r) => r.name.trim() && Number(r.port) > 0).map((r) => ({ name: r.name.trim(), port: Number(r.port) }));
      const g = await apiPost<CreateGatewayResponse>("/api/portal/customer/gateways", { label: label.trim(), services });
      setCreated(g);
      onCreated();
      toast.success("Gateway created", g.label);
    } catch (err) {
      setError(errorMessage(err, "Couldn't create the gateway"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      {created ? (
        <DialogContent
          title="Set up your gateway"
          description={`Four steps on the machine that runs next to ${created.label}'s services.`}
          className="max-w-2xl"
          footer={<Button onClick={() => close(false)}>Done</Button>}
        >
          <GatewayInstallSteps
            gateway={created}
            host={window.location.hostname}
            maxMbps={Number(mbps) || 0}
            connected={connectedIds.has(created.id)}
          />
        </DialogContent>
      ) : (
        <DialogContent
          title="New gateway"
          description="A gateway is a small container you run next to your data. Tasks can reach only the services you publish through it."
          footer={
            <>
              <Button variant="secondary" onClick={() => close(false)} type="button">
                Cancel
              </Button>
              <Button type="submit" form="create-gateway" loading={busy} disabled={!valid}>
                Create gateway
              </Button>
            </>
          }
        >
          <form id="create-gateway" onSubmit={submit} className="space-y-4 pb-2">
            {error && <Alert tone="danger">{error}</Alert>}
            <Field label="Name" hint="Something you'll recognise, e.g. production-postgres." required>
              <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="production-postgres" autoFocus />
            </Field>
            <div className="space-y-2">
              <p className="text-xs font-medium text-fg">Published services</p>
              {rows.map((r, i) => (
                <div key={i} className="flex items-center gap-2">
                  <Input
                    aria-label="Service name"
                    value={r.name}
                    onChange={(e) => setRows((cur) => cur.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
                    placeholder="postgres"
                    mono
                    className="flex-1"
                  />
                  <Input
                    aria-label="Port"
                    value={r.port}
                    inputMode="numeric"
                    onChange={(e) => setRows((cur) => cur.map((x, j) => (j === i ? { ...x, port: e.target.value.replace(/\D/g, "") } : x)))}
                    placeholder="5432"
                    mono
                    className="w-24"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label="Remove service"
                    disabled={rows.length === 1}
                    onClick={() => setRows((cur) => cur.filter((_, j) => j !== i))}
                  >
                    <Trash2 />
                  </Button>
                </div>
              ))}
              <Button type="button" variant="ghost" size="sm" onClick={() => setRows((cur) => [...cur, { name: "", port: "" }])}>
                <Plus />
                Add another service
              </Button>
              <p className="text-xs leading-5 text-muted">The port is where the service listens on the gateway's own machine.</p>
            </div>
            <div className="space-y-2">
              <p className="text-xs font-medium text-fg">Bandwidth limit</p>
              <div className="space-y-2">
                <Segmented<string>
                  label="Bandwidth limit"
                  value={mbps}
                  onChange={setMbps}
                  options={[
                    { value: "", label: "Unlimited" },
                    { value: "10", label: "10 Mbps" },
                    { value: "50", label: "50 Mbps" },
                    { value: "100", label: "100 Mbps" },
                    { value: "500", label: "500 Mbps" },
                  ]}
                />
                <div className="flex items-center gap-2">
                  <Input
                    aria-label="Custom bandwidth limit in Mbps"
                    value={mbps}
                    inputMode="numeric"
                    onChange={(e) => setMbps(e.target.value.replace(/\D/g, ""))}
                    placeholder="Custom"
                    mono
                    className="w-28"
                  />
                  <span className="text-xs text-muted">Mbps (megabits per second)</span>
                </div>
              </div>
              <p className="text-xs leading-5 text-muted">
                Enforced by the gateway itself: all tasks and both directions share it. Unlimited uses the machine's full link.
              </p>
            </div>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}

/** Delete is offered only while nothing unfinished uses the gateway (the server enforces it too). */
export function DeleteGatewayButton({
  gatewayId,
  label,
  tasks,
  onClick,
}: {
  gatewayId: string;
  label: string;
  tasks: Task[] | null | undefined;
  onClick: () => void;
}) {
  const a = gatewayActivity(tasks ?? [], gatewayId);
  const inUse = a.running.length + a.queued.length;
  const reason = inUse > 0 ? `${inUse} task${inUse === 1 ? "" : "s"} running or queued on it` : null;
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={reason ? `Delete ${label} (unavailable: ${reason})` : `Delete ${label}`}
      title={reason ? `Can't delete: ${reason}` : "Delete gateway"}
      disabled={reason !== null}
      onClick={onClick}
    >
      <Trash2 />
    </Button>
  );
}

export function Gateways() {
  usePageTitle("Gateways");
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);
  const [open, setOpen] = useState(false);
  const [deleting, setDeleting] = useState<Gateway | null>(null);
  const tasks = useAsync((s) => apiGet<Task[]>("/api/portal/customer/tasks", s), []);
  // Poll faster while the setup dialog is open so "Connected" appears as soon as the gateway calls in.
  useInterval(
    () => {
      gateways.reload();
      tasks.reload();
    },
    open ? 3_000 : 15_000
  );

  const list = gateways.data ?? [];
  const newButton = (
    <Button onClick={() => setOpen(true)}>
      <Plus />
      New gateway
    </Button>
  );

  return (
    <div className="space-y-6">
      <PageHeader
        title="Gateways"
        description="Programs you run next to your own services. Tasks can reach only what you publish through one."
        actions={newButton}
      />

      {gateways.error && !gateways.data ? (
        <Card>
          <ErrorState title="Couldn't load your gateways" message={gateways.error} onRetry={gateways.reload} />
        </Card>
      ) : gateways.loading ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(21rem,1fr))] gap-4">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-44 rounded-xl" />
          ))}
        </div>
      ) : list.length === 0 ? (
        <Card>
          <EmptyState
            icon={Network}
            title="No gateways yet"
            description="Tasks run with no network at all. A gateway is how a task reaches your database or API: you install it, you choose what it exposes, and it counts every byte."
            action={newButton}
          />
        </Card>
      ) : (
        <>
          <GatewaysOverview gateways={list} tasks={tasks.data ?? undefined} />
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(21rem,1fr))] gap-4">
            {list.map((g) => (
              <li key={g.id} className="min-w-0">
                <Card className="flex h-full flex-col">
                  <CardBody className="space-y-4">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex min-w-0 items-center gap-3">
                        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border bg-raised text-muted">
                          <Network className="size-4" aria-hidden />
                        </span>
                        <h2 className="truncate text-sm font-semibold text-fg">
                          <Link to={`/gateways/${g.id}`} className="hover:text-accent hover:underline">
                            {g.label}
                          </Link>
                        </h2>
                      </div>
                      <span className="flex shrink-0 flex-col items-end gap-1.5">
                        <ConnectionPill connected={g.connected} size="sm" />
                        <UpdateNotice kind="gateway" info={g} name={g.label} />
                      </span>
                    </div>

                    <div>
                      <p className="mb-1.5 text-xs font-medium text-muted">Published services</p>
                      {g.services.length === 0 ? (
                        <p className="text-xs text-subtle">None</p>
                      ) : (
                        <ul className="flex flex-wrap gap-1.5">
                          {g.services.map((s) => (
                            <li key={`${s.name}:${s.port}`} className="max-w-full">
                              <Badge tone="neutral" className="font-mono">
                                {s.name}:{s.port}
                              </Badge>
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>

                    <Link
                      to={`/gateways/${g.id}`}
                      className="block rounded-lg border border-border bg-raised/40 p-3 transition-colors hover:bg-raised/70"
                      aria-label={`Traffic for ${g.label}`}
                    >
                      <dl className="grid grid-cols-2 gap-3">
                        <div className="min-w-0">
                          <dt className="flex items-center gap-1.5 text-xs text-muted">
                            <ArrowDownToLine className="size-3.5 text-accent" aria-hidden />
                            Received from tasks
                          </dt>
                          <dd className="mt-0.5 truncate text-sm font-semibold text-fg" data-tnum>
                            {bytes(g.traffic.received_from_tasks_bytes)}
                          </dd>
                        </div>
                        <div className="min-w-0">
                          <dt className="flex items-center gap-1.5 text-xs text-muted">
                            <ArrowUpFromLine className="size-3.5 text-success" aria-hidden />
                            Sent to tasks
                          </dt>
                          <dd className="mt-0.5 truncate text-sm font-semibold text-fg" data-tnum>
                            {bytes(g.traffic.sent_to_tasks_bytes)}
                          </dd>
                        </div>
                      </dl>
                    </Link>
                  </CardBody>
                  <div className="border-t border-border px-5 py-4">
                    <GatewayTester gatewayId={g.id} update={<UpdateNotice kind="gateway" info={g} name={g.label} />} />
                  </div>
                  <div className="flex-1" aria-hidden />
                  <CardFooter>
                    <Identifier value={g.id} className="text-muted" />
                    <span className="flex shrink-0 items-center gap-2">
                      <span className="text-xs text-subtle">{relativeTime(g.created_at_ms)}</span>
                      <DeleteGatewayButton gatewayId={g.id} label={g.label} tasks={tasks.data} onClick={() => setDeleting(g)} />
                    </span>
                  </CardFooter>
                </Card>
              </li>
            ))}
          </ul>
        </>
      )}

      <p className="text-xs leading-5 text-muted">
        To let a task use a gateway, pick it under{" "}
        <Link to="/tasks/new" className="text-accent hover:underline">
          New task → Network access
        </Link>
        .
      </p>

      <DeleteGatewayDialog gateway={deleting} onClose={() => setDeleting(null)} onClosed={gateways.reload} />
      <CreateGatewayDialog
        open={open}
        onOpenChange={setOpen}
        onCreated={gateways.reload}
        connectedIds={new Set(list.filter((g) => g.connected).map((g) => g.id))}
      />
    </div>
  );
}
