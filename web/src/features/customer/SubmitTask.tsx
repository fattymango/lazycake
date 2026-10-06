import { useMemo, useState } from "react";
import type { FormEvent } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Plus, Sparkles, Trash2 } from "lucide-react";
import { apiGet, apiPost, errorMessage } from "@/lib/api";
import { cn } from "@/lib/cn";
import { cpu, memory, money, parseImage } from "@/lib/format";
import { useAsync } from "@/lib/hooks/useAsync";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { hasBalancedQuotes, joinArgs, splitArgs } from "@/lib/shellwords";
import type { Gateway, Me, SubmitTaskRequest, Task, TunnelTarget } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { Field, Input, Select } from "@/ui/Input";
import { KeyValueList } from "@/ui/KeyValue";
import { PageHeader } from "@/ui/PageHeader";
import { Segmented } from "@/ui/Segmented";
import { useToast } from "@/ui/Toast";
import { CORE_OPTIONS, MEMORY_OPTIONS, TIMEOUT_OPTIONS, presets, type TaskPreset } from "./presets";

const DIGEST_PINNED = /^[^\s@]+@sha256:[0-9a-f]{64}$/;
const MAX_TARGETS = 3;

interface EnvRow {
  key: string;
  value: string;
}
interface TargetRow {
  gatewayId: string;
  service: string;
}

interface FormState {
  image: string;
  command: string;
  env: EnvRow[];
  cores: number;
  memoryMB: number;
  timeoutS: number;
  targets: TargetRow[];
}

const blank: FormState = { image: "", command: "", env: [], cores: 0.5, memoryMB: 512, timeoutS: 300, targets: [] };

function fromPreset(p: TaskPreset): FormState {
  return {
    image: p.image,
    command: p.command,
    env: Object.entries(p.env ?? {}).map(([key, value]) => ({ key, value })),
    cores: p.cores,
    memoryMB: p.memoryMB,
    timeoutS: p.timeoutS,
    targets: [],
  };
}

function fromTask(t: Task): FormState {
  return {
    image: t.image,
    command: joinArgs([...(t.entrypoint ?? []), ...(t.args ?? [])]),
    env: Object.entries(t.env ?? {}).map(([key, value]) => ({ key, value })),
    cores: t.cores,
    memoryMB: t.memory_mb,
    timeoutS: t.wall_timeout_s ?? 300,
    // Kept as-is: a target whose gateway no longer exists simply drops out of
    // the request (see `targets` below), so nothing is submitted by accident.
    targets: (t.tunnel_targets ?? []).map((g) => ({ gatewayId: g.gateway_id, service: g.hostname })),
  };
}

export function SubmitTask() {
  usePageTitle("New task");
  const navigate = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const gateways = useAsync((s) => apiGet<Gateway[]>("/api/portal/customer/gateways", s), []);
  const me = useAsync((s) => apiGet<Me>("/api/portal/customer/me", s), []);

  const rerunOf = (location.state as { from?: Task } | null)?.from;
  const [form, setForm] = useState<FormState>(() => (rerunOf ? fromTask(rerunOf) : blank));
  const [activePreset, setActivePreset] = useState<string | null>(null);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setForm((f) => ({ ...f, [k]: v }));
  const gws = gateways.data ?? [];

  const problems = useMemo(() => {
    const p: Record<string, string> = {};
    if (!form.image.trim()) p.image = "Enter an image.";
    else if (!DIGEST_PINNED.test(form.image.trim())) p.image = "Images must be pinned by digest: name@sha256:… (64 hex characters).";
    if (!hasBalancedQuotes(form.command)) p.command = "There's an unclosed quote.";
    const keys = form.env.map((e) => e.key.trim()).filter(Boolean);
    if (new Set(keys).size !== keys.length) p.env = "Each variable name can be used once.";
    return p;
  }, [form.image, form.command, form.env]);

  const targets: TunnelTarget[] = form.targets.flatMap((t) => {
    const g = gws.find((x) => x.id === t.gatewayId);
    const svc = g?.services.find((s) => s.name === t.service);
    return g && svc ? [{ gateway_id: g.id, hostname: svc.name, port: svc.port }] : [];
  });

  function applyPreset(p: TaskPreset) {
    setActivePreset(p.id);
    setForm(fromPreset(p));
    setTouched(false);
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setTouched(true);
    setError(null);
    if (Object.keys(problems).length) return;
    setBusy(true);
    try {
      const argv = splitArgs(form.command);
      const env = Object.fromEntries(form.env.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.value]));
      const body: SubmitTaskRequest = {
        image: form.image.trim(),
        args: argv.length ? argv : undefined,
        env: Object.keys(env).length ? env : undefined,
        cores: form.cores,
        memory_mb: form.memoryMB,
        wall_timeout_s: form.timeoutS,
        tunnel_targets: targets.length ? targets : undefined,
      };
      const task = await apiPost<Task>("/api/portal/customer/tasks", body);
      toast.success("Task submitted", "It's queued and will start as soon as a node has room.");
      navigate(`/tasks/${task.id}`);
    } catch (err) {
      setError(errorMessage(err, "Couldn't submit the task"));
      setBusy(false);
    }
  }

  const image = form.image.trim() ? parseImage(form.image.trim()) : null;
  const argv = splitArgs(form.command);
  const showErr = (k: string) => (touched ? problems[k] : undefined);

  return (
    <form onSubmit={submit} className="space-y-6" noValidate>
      <PageHeader
        title="New task"
        description="Run a container on a host's machine. It gets exactly the resources you pick, and nothing else."
      />

      {rerunOf && (
        <Alert tone="info" title="Prefilled from an earlier task">
          Settings are copied from <span className="font-mono">{rerunOf.id.slice(0, 12)}…</span>. Change anything before you submit.
        </Alert>
      )}
      {error && (
        <Alert tone="danger" title="Couldn't submit this task">
          {error}
        </Alert>
      )}

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_21rem]">
        <div className="min-w-0 space-y-6">
          <Card>
            <CardHeader title="Start from a template" description="Pick one to fill in the form, then adjust." />
            <CardBody className="grid gap-3 pt-4 sm:grid-cols-3">
              {presets.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => applyPreset(p)}
                  className={cn(
                    "flex min-w-0 flex-col items-start gap-1.5 rounded-xl border p-3.5 text-left transition-[border-color,background-color]",
                    activePreset === p.id
                      ? "border-accent/60 bg-accent/[0.06]"
                      : "border-border bg-surface hover:border-border-strong hover:bg-raised/40"
                  )}
                >
                  <span className="flex items-center gap-2 text-sm font-semibold text-fg">
                    <Sparkles className={cn("size-3.5", activePreset === p.id ? "text-accent" : "text-muted")} aria-hidden />
                    {p.name}
                  </span>
                  <span className="text-xs leading-5 text-muted">{p.description}</span>
                </button>
              ))}
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="What to run" />
            <CardBody className="space-y-5 pt-4">
              <Field
                label="Image"
                required
                error={showErr("image")}
                hint="Pinned by digest so every node runs byte-for-byte the same thing."
              >
                <Input
                  mono
                  value={form.image}
                  onChange={(e) => set("image", e.target.value)}
                  placeholder="docker.io/library/alpine@sha256:c64c…"
                  spellCheck={false}
                  autoCapitalize="none"
                />
              </Field>
              <Field
                label="Command"
                error={showErr("command")}
                hint='Leave empty to use the image&apos;s own command. Quotes group words: sh -c "sleep 5; echo done".'
              >
                <Input
                  mono
                  value={form.command}
                  onChange={(e) => set("command", e.target.value)}
                  placeholder="python -m job --batch 500"
                  spellCheck={false}
                  autoCapitalize="none"
                />
              </Field>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-fg">Environment variables</p>
                  <Button type="button" variant="ghost" size="sm" onClick={() => set("env", [...form.env, { key: "", value: "" }])}>
                    <Plus />
                    Add variable
                  </Button>
                </div>
                {form.env.length === 0 && (
                  <p className="text-xs text-muted">None. Variables are visible to the host, so don't put secrets here.</p>
                )}
                {form.env.map((row, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <Input
                      mono
                      aria-label="Variable name"
                      placeholder="NAME"
                      value={row.key}
                      onChange={(e) =>
                        set(
                          "env",
                          form.env.map((r, j) => (j === i ? { ...r, key: e.target.value } : r))
                        )
                      }
                      className="w-2/5"
                    />
                    <Input
                      mono
                      aria-label="Variable value"
                      placeholder="value"
                      value={row.value}
                      onChange={(e) =>
                        set(
                          "env",
                          form.env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r))
                        )
                      }
                      className="flex-1"
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label="Remove variable"
                      onClick={() =>
                        set(
                          "env",
                          form.env.filter((_, j) => j !== i)
                        )
                      }
                    >
                      <Trash2 />
                    </Button>
                  </div>
                ))}
                {showErr("env") && (
                  <p role="alert" className="text-xs text-danger">
                    {showErr("env")}
                  </p>
                )}
              </div>
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Resources" description="Enforced by the host's kernel. The task can't use more." />
            <CardBody className="space-y-5 pt-4">
              <div className="space-y-1.5">
                <p className="text-xs font-medium text-fg">CPU</p>
                <Segmented
                  label="CPU cores"
                  value={String(form.cores)}
                  onChange={(v) => set("cores", Number(v))}
                  options={CORE_OPTIONS.map((c) => ({ value: String(c), label: `${c} vCPU` }))}
                />
              </div>
              <div className="space-y-1.5">
                <p className="text-xs font-medium text-fg">Memory</p>
                <Segmented
                  label="Memory"
                  value={String(form.memoryMB)}
                  onChange={(v) => set("memoryMB", Number(v))}
                  options={MEMORY_OPTIONS.map((m) => ({ value: String(m), label: memory(m) }))}
                />
              </div>
              <Field
                label="Time limit"
                hint="The task is stopped when this runs out. Your balance is held for the full limit up front and refunded for unused time."
              >
                <Select value={String(form.timeoutS)} onChange={(e) => set("timeoutS", Number(e.target.value))} className="max-w-[12rem]">
                  {TIMEOUT_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </Select>
              </Field>
            </CardBody>
          </Card>

          <Card>
            <CardHeader
              title="Network access"
              description="Tasks have no network by default. Allow up to three of your gateway's services."
            />
            <CardBody className="space-y-3 pt-4">
              {gateways.loading ? (
                <p className="text-sm text-muted">Loading your gateways…</p>
              ) : gws.length === 0 ? (
                <p className="text-sm text-muted">
                  You have no gateways yet.{" "}
                  <Link to="/gateways" className="text-accent hover:underline">
                    Create one
                  </Link>{" "}
                  if this task needs to reach your own services.
                </p>
              ) : (
                <>
                  {form.targets.map((t, i) => {
                    const g = gws.find((x) => x.id === t.gatewayId);
                    return (
                      <div key={i} className="flex flex-wrap items-center gap-2 sm:flex-nowrap">
                        <Select
                          aria-label="Gateway"
                          value={t.gatewayId}
                          onChange={(e) => {
                            const next = gws.find((x) => x.id === e.target.value);
                            set(
                              "targets",
                              form.targets.map((r, j) =>
                                j === i ? { gatewayId: e.target.value, service: next?.services[0]?.name ?? "" } : r
                              )
                            );
                          }}
                          className="min-w-0 flex-1"
                        >
                          {gws.map((x) => (
                            <option key={x.id} value={x.id}>
                              {x.label}
                              {x.connected ? "" : " (offline)"}
                            </option>
                          ))}
                        </Select>
                        <Select
                          aria-label="Service"
                          value={t.service}
                          onChange={(e) =>
                            set(
                              "targets",
                              form.targets.map((r, j) => (j === i ? { ...r, service: e.target.value } : r))
                            )
                          }
                          className="min-w-0 flex-1"
                        >
                          {(g?.services ?? []).map((s) => (
                            <option key={s.name} value={s.name}>
                              {s.name}:{s.port}
                            </option>
                          ))}
                        </Select>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          aria-label="Remove target"
                          onClick={() =>
                            set(
                              "targets",
                              form.targets.filter((_, j) => j !== i)
                            )
                          }
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    );
                  })}
                  {form.targets.length < MAX_TARGETS && (
                    <Button
                      type="button"
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        const g = gws[0];
                        set("targets", [...form.targets, { gatewayId: g.id, service: g.services[0]?.name ?? "" }]);
                      }}
                    >
                      <Plus />
                      Allow a service
                    </Button>
                  )}
                </>
              )}
            </CardBody>
          </Card>
        </div>

        <aside className="min-w-0 xl:sticky xl:top-20">
          <Card>
            <CardHeader title="Summary" />
            <CardBody className="space-y-5 pt-4">
              <KeyValueList
                items={[
                  {
                    label: "Image",
                    value: image ? (
                      <span className="font-mono text-xs">
                        {image.name}
                        <span className="text-muted"> · {image.digest || "no digest"}</span>
                      </span>
                    ) : (
                      <span className="text-muted">Not set</span>
                    ),
                  },
                  {
                    label: "Command",
                    value: argv.length ? (
                      <span className="font-mono text-xs break-all">{argv.join(" ")}</span>
                    ) : (
                      <span className="text-muted">Image default</span>
                    ),
                  },
                  { label: "CPU", value: <span data-tnum>{cpu(form.cores)}</span> },
                  { label: "Memory", value: <span data-tnum>{memory(form.memoryMB)}</span> },
                  { label: "Time limit", value: TIMEOUT_OPTIONS.find((o) => o.value === form.timeoutS)?.label ?? `${form.timeoutS}s` },
                  {
                    label: "Network",
                    value: targets.length ? (
                      `${targets.length} service${targets.length > 1 ? "s" : ""}`
                    ) : (
                      <span className="text-muted">None</span>
                    ),
                  },
                ]}
              />
              <div className="rounded-lg border border-border bg-raised/50 px-3.5 py-3 text-xs leading-5 text-muted">
                Available balance{" "}
                <span className="font-semibold text-fg" data-tnum>
                  {me.data ? money(me.data.available_balance_micros) : "…"}
                </span>
              </div>
              <Button type="submit" size="lg" className="w-full" loading={busy}>
                Submit task
              </Button>
            </CardBody>
          </Card>
        </aside>
      </div>
    </form>
  );
}
