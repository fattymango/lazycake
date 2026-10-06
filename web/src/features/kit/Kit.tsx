import { useState } from "react";
import { Activity, Cpu, DollarSign, Layers, Plus, Trash2 } from "lucide-react";
import { Alert } from "@/ui/Alert";
import { Badge } from "@/ui/Badge";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardFooter, CardHeader } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
import { DataTable } from "@/ui/DataTable";
import { ConfirmDialog, Dialog, DialogContent, DialogTrigger } from "@/ui/Dialog";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Identifier, Truncate } from "@/ui/Identifier";
import { Field, Input, Select, Textarea } from "@/ui/Input";
import { KeyValueList } from "@/ui/KeyValue";
import { PageHeader } from "@/ui/PageHeader";
import { Pagination } from "@/ui/Pagination";
import { ProgressBar } from "@/ui/ProgressBar";
import { Segmented } from "@/ui/Segmented";
import { Skeleton } from "@/ui/Skeleton";
import { StatCard } from "@/ui/StatCard";
import { ConnectionPill, TaskStatusPill } from "@/ui/StatusPill";
import { ThemeToggle } from "@/ui/ThemeToggle";
import { Toggle } from "@/ui/Toggle";
import { useToast } from "@/ui/Toast";
import { taskStatus } from "@/ui/status";
import type { TaskState } from "@/lib/types";

// Dev-only gallery (mounted at /_kit): every primitive in one place, with
// adversarial content (very long IDs) so overflow is caught by eye and by
// e2e/shoot.mjs. When you add a primitive, add it here.
const LONG_ID = "tsk_01M4170DCF1506FFA2A58939F6AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
const LONG_IMG = "docker.io/fattymango/lcbench@sha256:65e8c5765cc43af8cf9e6a71165d31954dbfee495aa1ada997fb35b8c672e1db";

const rows = (["queued", "running", "succeeded", "failed", "fenced"] as TaskState[]).map((s, i) => ({
  id: `tsk_01M4${i}DCF1506FFA2A58939F${i}`,
  state: s,
  image: LONG_IMG,
}));

export function Kit() {
  const toast = useToast();
  const [seg, setSeg] = useState("all");
  const [confirm, setConfirm] = useState(false);
  const [wrap, setWrap] = useState(true);

  return (
    <div className="mx-auto max-w-[75rem] space-y-10 px-4 py-8 sm:px-8">
      <PageHeader
        title="Component kit"
        description="Every primitive in both themes, with deliberately awkward content."
        actions={<ThemeToggle />}
      />

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">Buttons</h2>
        <div className="flex flex-wrap items-center gap-3">
          <Button>Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger">Danger</Button>
          <Button variant="danger-solid">Delete</Button>
          <Button loading>Saving</Button>
          <Button disabled>Disabled</Button>
          <Button size="sm">
            <Plus />
            Small
          </Button>
          <Button size="lg">Large</Button>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">Status</h2>
        <div className="flex flex-wrap gap-2">
          {(Object.keys(taskStatus) as TaskState[]).map((s) => (
            <TaskStatusPill key={s} state={s} />
          ))}
          <ConnectionPill connected />
          <ConnectionPill connected={false} />
          <Badge tone="accent">Customer</Badge>
          <Badge tone="info" size="sm">
            amd64
          </Badge>
        </div>
      </section>

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="Available balance"
          value="$24.32"
          sub="of $24.32 total"
          icon={DollarSign}
          tone="success"
          trend={[3, 5, 4, 8, 6, 9, 12, 11]}
        />
        <StatCard label="Running now" value="3" sub="2 vCPU in use" icon={Activity} tone="accent" />
        <StatCard label="Queued" value="2" icon={Layers} />
        <StatCard label="Loading" value="" icon={Cpu} loading />
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader
            title="Forms"
            description="Labels, hints and errors are wired for screen readers."
            action={
              <Button size="sm" variant="secondary">
                Action
              </Button>
            }
          />
          <CardBody className="space-y-4">
            <Field label="Image" hint="Must be pinned by digest (@sha256:…)." required>
              <Input mono placeholder="docker.io/library/alpine@sha256:…" />
            </Field>
            <Field label="With an error" error="That username is already taken.">
              <Input defaultValue="alice" />
            </Field>
            <Field label="Cores">
              <Select defaultValue="0.5">
                <option value="0.25">0.25</option>
                <option value="0.5">0.5</option>
                <option value="1">1</option>
              </Select>
            </Field>
            <Field label="Command">
              <Textarea mono placeholder="sleep 60" />
            </Field>
          </CardBody>
          <CardFooter>
            <span className="text-xs text-muted">Footer text</span>
            <Button size="sm">Save</Button>
          </CardFooter>
        </Card>

        <Card>
          <CardHeader title="Identifiers & truncation" description="Long values shorten, show in full on hover, and copy in one click." />
          <CardBody className="space-y-4">
            <KeyValueList
              items={[
                { label: "Task", value: <Identifier value="tsk_01M4170DCF1506FFA2A58939F6" /> },
                { label: "Absurd ID", value: <Identifier value={LONG_ID} /> },
                { label: "Full", value: <Identifier value={LONG_ID} full /> },
                { label: "Image", value: <Truncate>{LONG_IMG}</Truncate> },
                { label: "Wrapping", value: LONG_ID },
              ]}
            />
            <div className="w-40 rounded-lg border border-dashed border-border p-2">
              <Identifier value={LONG_ID} />
              <p className="mt-1 text-2xs text-muted">in a 160px box</p>
            </div>
          </CardBody>
        </Card>
      </section>

      <Card>
        <CardHeader
          title="Data table"
          description="Fixed layout: a cell can't stretch it."
          action={
            <Segmented
              label="Filter"
              value={seg}
              onChange={setSeg}
              options={[
                { value: "all", label: "All", count: 5 },
                { value: "active", label: "Active", count: 2 },
                { value: "done", label: "Done", count: 3 },
              ]}
            />
          }
        />
        <div className="mt-4 border-t border-border">
          <DataTable
            caption="Example tasks"
            columns={[
              { id: "id", header: "Task", cell: (r) => <Identifier value={r.id} />, width: "14rem" },
              { id: "state", header: "State", cell: (r) => <TaskStatusPill state={r.state} />, width: "9rem" },
              { id: "image", header: "Image", cell: (r) => <Truncate className="font-mono text-xs text-muted">{r.image}</Truncate> },
            ]}
            rows={rows}
            getRowKey={(r) => r.id}
            onRowClick={() => toast.success("Row clicked")}
          />
          <Pagination page={1} pageSize={5} total={42} onChange={() => {}} />
        </div>
      </Card>

      <section className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader title="Code & progress" />
          <CardBody className="space-y-4">
            <CodeBlock
              label="Shell"
              code={
                "mkdir -p $HOME/.local/share/lazycake/bin && podman run -d --replace --name lazycake-agent --pid=host --cap-add=SYS_ADMIN -e LAZYCAKE_TOKEN=0fa39f6fbcb365d66a825c00b9fa12f311239c76c2e72751727fc54942717bcf docker.io/fattymango/lazycake-agent:latest"
              }
            />
            <div className="space-y-3">
              <ProgressBar label="Cores" value={0.62} />
              <ProgressBar label="Memory" value={0.91} tone="warning" />
              <ProgressBar label="Disk" value={0.2} tone="success" />
            </div>
            <Toggle pressed={wrap} onClick={() => setWrap((w) => !w)}>
              Wrap lines
            </Toggle>
          </CardBody>
        </Card>
        <Card>
          <CardHeader title="Feedback" />
          <CardBody className="space-y-3">
            <Alert tone="info" title="Heads up">
              Gateways can reach at most three registered targets.
            </Alert>
            <Alert tone="warning" title="Offline">
              This machine hasn't sent a heartbeat in 5 minutes.
            </Alert>
            <Alert tone="danger" title="Couldn't submit">
              Insufficient balance for this task.
            </Alert>
            <Alert tone="success" title="Saved" />
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" onClick={() => toast.success("Copied to clipboard")}>
                Success toast
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => toast.error("Couldn't reach the server", "Check your connection and retry.")}
              >
                Error toast
              </Button>
              <Dialog>
                <DialogTrigger asChild>
                  <Button variant="secondary" size="sm">
                    Open dialog
                  </Button>
                </DialogTrigger>
                <DialogContent
                  title="Create gateway"
                  description="Gateways let tasks reach services on your network."
                  footer={<Button>Create</Button>}
                >
                  <Field label="Label">
                    <Input placeholder="production-postgres" />
                  </Field>
                </DialogContent>
              </Dialog>
              <Button variant="danger" size="sm" onClick={() => setConfirm(true)}>
                <Trash2 />
                Confirm
              </Button>
            </div>
            <div className="space-y-2">
              <Skeleton className="h-4 w-2/3" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          </CardBody>
        </Card>
      </section>

      <Card>
        <EmptyState
          icon={Layers}
          title="No tasks yet"
          description="Submit your first container and watch it run on a node."
          action={
            <Button>
              <Plus />
              New task
            </Button>
          }
        />
        <div className="border-t border-border">
          <ErrorState title="Couldn't load tasks" message="The server returned an unexpected error." onRetry={() => {}} compact />
        </div>
      </Card>

      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Remove this machine?"
        description="It will stop receiving tasks. Its earnings history is kept."
        confirmLabel="Remove"
        danger
        onConfirm={() => setConfirm(false)}
      />
    </div>
  );
}
