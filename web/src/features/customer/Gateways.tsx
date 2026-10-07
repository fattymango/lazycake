import { useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router-dom";
import { ArrowDownToLine, ArrowUpFromLine, Network, Plus, Trash2 } from "lucide-react";
import { apiGet, apiPost, errorMessage } from "@/lib/api";
import { gatewayInstallCommand } from "@/lib/gatewayInstall";
import { bytes, relativeTime } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { useInterval } from "@/lib/hooks/useInterval";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import type { CreateGatewayResponse, Gateway } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardFooter } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
import { Dialog, DialogContent } from "@/ui/Dialog";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier } from "@/ui/Identifier";
import { Field, Input } from "@/ui/Input";
import { PageHeader } from "@/ui/PageHeader";
import { GatewayTester } from "./GatewayTest";
import { Skeleton } from "@/ui/Skeleton";
import { ConnectionPill } from "@/ui/StatusPill";
import { useToast } from "@/ui/Toast";

interface ServiceRow {
  name: string;
  port: string;
}

function CreateGatewayDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onCreated: () => void;
}) {
  const toast = useToast();
  const [label, setLabel] = useState("");
  const [rows, setRows] = useState<ServiceRow[]>([{ name: "", port: "" }]);
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
          title="Install your gateway"
          description="Run this on the machine that can reach your service (it needs podman). It connects out to LazyCake, so no inbound ports are needed."
          className="max-w-xl"
          footer={<Button onClick={() => close(false)}>Done</Button>}
        >
          <div className="space-y-4 pb-2">
            <Alert tone="warning" title="Copy the token now">
              It's only shown once. If you lose it, delete this gateway and create a new one.
            </Alert>
            <CodeBlock label="Shell" code={gatewayInstallCommand(created, window.location.hostname)} />
            <p className="text-xs leading-5 text-muted">
              The gateway runs as a container with host networking, so it can reach your services on that machine's own localhost. Ports{" "}
              <span className="font-mono">7443</span> (gRPC) and <span className="font-mono">7444</span> (UDP, QUIC) on the coordinator must
              be reachable from that machine.
            </p>
          </div>
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
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}

export function Gateways() {
  usePageTitle("Gateways");
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);
  const [open, setOpen] = useState(false);
  useInterval(() => gateways.reload(), 15_000);

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
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
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
        <ul className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
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
                    <ConnectionPill connected={g.connected} size="sm" />
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
                  <GatewayTester gatewayId={g.id} />
                </div>
                <div className="flex-1" aria-hidden />
                <CardFooter>
                  <Identifier value={g.id} className="text-muted" />
                  <span className="shrink-0 text-xs text-subtle">{relativeTime(g.created_at_ms)}</span>
                </CardFooter>
              </Card>
            </li>
          ))}
        </ul>
      )}

      <p className="text-xs leading-5 text-muted">
        To let a task use a gateway, pick it under{" "}
        <Link to="/tasks/new" className="text-accent hover:underline">
          New task → Network access
        </Link>
        .
      </p>

      <CreateGatewayDialog open={open} onOpenChange={setOpen} onCreated={gateways.reload} />
    </div>
  );
}
