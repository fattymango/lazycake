import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  ArrowRight,
  CheckCircle2,
  Cpu,
  Gauge,
  HardDrive,
  KeyRound,
  Loader2,
  MemoryStick,
  ServerCog,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import { apiPost, errorMessage } from "@/lib/api";
import { cn } from "@/lib/cn";
import {
  CAPACITY_COMMAND,
  OFFER_BOUNDS,
  defaultOffer,
  maxOffer,
  offerProblems,
  offerRequest,
  parseCapacity,
  type MachineCaps,
  type OfferField,
  type OfferInput,
} from "@/lib/offer";
import { useLiveEvents } from "@/lib/live";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import type { InstallToken } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Card, CardBody } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
import { Field, Input, Textarea } from "@/ui/Input";
import { PageHeader } from "@/ui/PageHeader";

function Step({ n, title, done, last, children }: { n: number; title: string; done?: boolean; last?: boolean; children: React.ReactNode }) {
  return (
    <li className="relative flex gap-4 pb-8 last:pb-0">
      {!last && <div className="absolute left-4 top-9 h-[calc(100%-2.25rem)] w-px bg-border" aria-hidden />}
      <span
        className={cn(
          "relative z-10 flex size-8 shrink-0 items-center justify-center rounded-full border text-sm font-semibold",
          done ? "border-success/40 bg-success/15 text-success" : "border-border-strong bg-raised text-fg"
        )}
      >
        {done ? <CheckCircle2 className="size-4" aria-hidden /> : n}
      </span>
      <div className="min-w-0 flex-1 pt-1">
        <h2 className="text-sm font-semibold text-fg">{title}</h2>
        <div className="mt-3 space-y-3">{children}</div>
      </div>
    </li>
  );
}

const OFFER_ROWS: { field: OfferField; label: string; unit: string; icon: typeof Cpu; step: number; hint: string }[] = [
  { field: "cores", label: "CPU", unit: "cores", icon: Cpu, step: 0.25, hint: "CPU cores customers' tasks can use at the same time." },
  { field: "memoryGB", label: "Memory", unit: "GB", icon: MemoryStick, step: 0.25, hint: "RAM shared by all tasks running here." },
  { field: "storageGB", label: "Storage", unit: "GB", icon: HardDrive, step: 1, hint: "Disk for task scratch space and cached images." },
  {
    field: "networkMbps",
    label: "Network",
    unit: "Mbps",
    icon: Gauge,
    step: 10,
    hint: "Bandwidth for task traffic through gateways, all tasks together.",
  },
];

function OfferControls({
  offer,
  caps,
  problems,
  onChange,
}: {
  offer: OfferInput;
  caps: MachineCaps | null;
  problems: Partial<Record<OfferField, string>>;
  onChange: (next: OfferInput) => void;
}) {
  const max = maxOffer(caps);
  return (
    <ul className="space-y-5">
      {OFFER_ROWS.map(({ field, label, unit, icon: Icon, step, hint }) => {
        const bound = OFFER_BOUNDS[field];
        const known = field === "networkMbps" ? caps?.networkMbps != null : !!caps;
        const top = Math.floor(max[field] * 100) / 100;
        const value = offer[field];
        const error = problems[field];
        return (
          <li key={field}>
            <Field
              label={
                <span className="flex items-center gap-2">
                  <Icon className="size-3.5 text-muted" aria-hidden />
                  {label}
                </span>
              }
              hint={
                known
                  ? `${hint} This machine has ${field === "networkMbps" ? `a ${top} Mbps link` : `${top} ${unit}${field === "storageGB" ? " free" : ""}`}.`
                  : hint
              }
              error={error}
            >
              <div className="flex items-center gap-3">
                <input
                  type="range"
                  aria-label={`${label} slider`}
                  min={bound.min}
                  max={known ? top : Math.min(bound.max, field === "cores" ? 64 : field === "networkMbps" ? 10_000 : 1024)}
                  step={step}
                  value={Number.isFinite(value) ? Math.min(value, known ? top : bound.max) : bound.min}
                  onChange={(e) => onChange({ ...offer, [field]: Number(e.target.value) })}
                  className="h-1.5 min-w-0 flex-1 cursor-pointer accent-[rgb(var(--accent))]"
                />
                <div className="flex w-36 shrink-0 items-center gap-2">
                  <Input
                    aria-label={`${label} to lend`}
                    inputMode="decimal"
                    type="number"
                    min={bound.min}
                    step={step}
                    value={Number.isFinite(value) ? value : ""}
                    onChange={(e) => onChange({ ...offer, [field]: e.target.value === "" ? Number.NaN : Number(e.target.value) })}
                    aria-invalid={!!error}
                    mono
                  />
                  <span className="w-10 shrink-0 text-xs text-muted">{unit}</span>
                </div>
              </div>
            </Field>
          </li>
        );
      })}
    </ul>
  );
}

export function AddMachine() {
  usePageTitle("Add a machine");
  const [result, setResult] = useState<InstallToken | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [connectedId, setConnectedId] = useState<string | null>(null);
  const [waitedLong, setWaitedLong] = useState(false);
  const [capsText, setCapsText] = useState("");
  const caps = parseCapacity(capsText);
  const [offer, setOffer] = useState<OfferInput>(defaultOffer(null));
  const problems = offerProblems(offer, caps);
  const valid = Object.keys(problems).length === 0;

  // Telling us what the machine has pulls the defaults, and anything already typed, down to what it can give.
  function onCapsChange(text: string) {
    setCapsText(text);
    const next = parseCapacity(text);
    if (!next) return;
    const max = maxOffer(next);
    setOffer((cur) => ({
      cores: Math.min(cur.cores, Math.floor(max.cores * 100) / 100),
      memoryGB: Math.min(cur.memoryGB, Math.floor(max.memoryGB * 100) / 100),
      storageGB: Math.min(cur.storageGB, Math.floor(max.storageGB * 100) / 100),
      networkMbps: Math.min(cur.networkMbps, Math.floor(max.networkMbps)),
    }));
  }

  // An agent runs inside a container, where 127.0.0.1 is the container itself,
  // so a command pointing at a loopback address can never connect from another
  // machine, or from the agent's own container. It happens when the coordinator
  // is run locally without LAZYCAKE_PUBLIC_GRPC_ADDR set.
  const loopback = !!result && /LAZYCAKE_COORDINATOR_ADDR=(127\.|localhost|\[?::1)/.test(result.install_command);

  // After a while with no connection, stop saying "wait" and say what to check.
  useEffect(() => {
    setWaitedLong(false);
    if (!result || connectedId) return;
    const t = window.setTimeout(() => setWaitedLong(true), 40_000);
    return () => window.clearTimeout(t);
  }, [result, connectedId]);

  // A machine that registers after we minted a token is almost certainly this one.
  useLiveEvents((e) => {
    if (result && e.type === "node_connected") setConnectedId(e.node_id);
  });

  async function generate() {
    setBusy(true);
    setError(null);
    setConnectedId(null);
    try {
      setResult(await apiPost<InstallToken>("/api/portal/provider/nodes/install-token", offerRequest(offer)));
    } catch (err) {
      setError(errorMessage(err, "Couldn't create an install token"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Add a machine"
        description="Register a computer and start earning when customers' tasks run on it. About two minutes."
      />

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_21rem]">
        <Card>
          <CardBody className="p-6">
            <ol>
              <Step n={1} title="Check the machine is ready" done={!!result}>
                <p className="text-sm leading-6 text-muted">
                  Any Linux machine with <span className="font-medium text-fg">rootless Podman</span> and{" "}
                  <span className="font-medium text-fg">cgroup v2</span> works. Run this on it to check:
                </p>
                <CodeBlock
                  label="Shell"
                  code={
                    'podman info --format json | grep -E \'"(cgroupVersion|rootless)"\'\n# expect:  "cgroupVersion": "v2",   "rootless": true,'
                  }
                />
                <p className="text-xs leading-5 text-muted">
                  Also run <span className="font-mono">loginctl enable-linger $USER</span> so the agent keeps running after you log out.
                </p>
              </Step>

              <Step n={2} title="Choose what to lend" done={!!result}>
                <p className="text-sm leading-6 text-muted">
                  You decide how much of this machine customers can use. The agent only ever uses what you set here, and it{" "}
                  <span className="font-medium text-fg">refuses to start</span> if you offer more than the machine really has.
                </p>
                <div className="space-y-2 rounded-lg border border-border bg-raised/40 p-3">
                  <p className="text-xs font-medium text-fg">See what this machine has (optional, recommended)</p>
                  <p className="text-xs leading-5 text-muted">
                    Run this on the machine, then paste its one line of output. The limits below will match the hardware.
                  </p>
                  <CodeBlock label="Shell" code={CAPACITY_COMMAND} wrap />
                  <Textarea
                    aria-label="Output of the capacity command"
                    value={capsText}
                    onChange={(e) => onCapsChange(e.target.value)}
                    placeholder="cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000"
                    className="min-h-[52px]"
                    mono
                  />
                  {capsText.trim() !== "" && !caps && (
                    <p className="text-xs text-danger">That doesn't look like the command's output. It should start with "cores=".</p>
                  )}
                  {caps && (
                    <p className="text-xs text-success">
                      Got it: {caps.cores} cores, {Math.floor((caps.memoryMB / 1024) * 100) / 100} GB memory,{" "}
                      {Math.floor((caps.diskMB / 1024) * 100) / 100} GB free
                      {caps.networkMbps ? `, ${caps.networkMbps} Mbps link` : ", network speed not reported (your limit is still enforced)"}
                      .
                    </p>
                  )}
                </div>
                <OfferControls offer={offer} caps={caps} problems={problems} onChange={setOffer} />
                {!caps && (
                  <p className="text-xs leading-5 text-muted">
                    Without the check above we can't know the machine's limits here, so the agent does that check itself when it starts.
                  </p>
                )}
              </Step>

              <Step n={3} title="Create an install command" done={!!result}>
                {error && <Alert tone="danger">{error}</Alert>}
                {!result ? (
                  <>
                    <p className="text-sm leading-6 text-muted">
                      This mints a token that lets one agent register as yours, and gives you the command to start it with the limits you
                      chose.
                    </p>
                    <Button onClick={generate} loading={busy} disabled={!valid}>
                      <KeyRound />
                      Generate install command
                    </Button>
                    {!valid && <p className="text-xs text-danger">Fix the highlighted limits above first.</p>}
                  </>
                ) : (
                  <>
                    <Alert tone="warning" title="Copy this now">
                      The token is shown once. Anyone with it can register a machine to your account. If it leaks or you lose it, generate a
                      new one.
                    </Alert>
                    {loopback && (
                      <Alert tone="danger" title="This command points at 127.0.0.1, so it can't connect">
                        The agent runs in a container, where 127.0.0.1 is the container itself, not the machine running LazyCake. The
                        coordinator needs to advertise an address your machines can reach: set{" "}
                        <span className="font-mono">LAZYCAKE_PUBLIC_GRPC_ADDR</span> (for example{" "}
                        <span className="font-mono">203.0.113.5:7443</span>) and generate a new command.
                      </Alert>
                    )}
                    <CodeBlock label="Run on your machine" code={result.install_command} />
                    <p className="text-xs leading-5 text-muted">
                      It offers {offer.cores} CPU cores, {offer.memoryGB} GB of memory, {offer.storageGB} GB of storage and{" "}
                      {offer.networkMbps} Mbps of network. If a value is more than the machine has, the agent stops with a message saying
                      which one to lower (see <span className="font-mono">podman logs lazycake-agent</span>).
                    </p>
                    <Button variant="ghost" size="sm" onClick={generate} loading={busy} disabled={!valid}>
                      Generate a new one
                    </Button>
                  </>
                )}
              </Step>

              <Step n={4} title="Wait for it to connect" done={!!connectedId} last>
                {connectedId ? (
                  <>
                    <Alert tone="success" title="Your machine is connected">
                      It registered, ran a quick benchmark and is ready for work.
                    </Alert>
                    <Button asChild>
                      <Link to={`/machines/${connectedId}`}>
                        View machine
                        <ArrowRight />
                      </Link>
                    </Button>
                  </>
                ) : result ? (
                  <>
                    <p className="flex items-center gap-2.5 text-sm text-muted">
                      <Loader2 className="size-4 animate-spin text-accent" aria-hidden />
                      Waiting for the agent to connect. This page updates by itself.
                    </p>
                    {waitedLong && (
                      <Alert tone="warning" title="Still waiting? Check these">
                        <ol className="mt-1 list-decimal space-y-1.5 pl-4">
                          <li>
                            Read why it isn't connecting:
                            <CodeBlock code="podman logs lazycake-agent" className="mt-1.5" />
                          </li>
                          <li>
                            The machine must reach the coordinator on <span className="font-mono">7443</span> (TCP) to connect, and{" "}
                            <span className="font-mono">7444</span> (UDP) for gateway tunnels. Check any firewall in between.
                          </li>
                          <li>The command's address must be one this machine can reach (not 127.0.0.1 or localhost).</li>
                          <li>If this machine already runs an agent, running the command again replaces it (same container name).</li>
                        </ol>
                      </Alert>
                    )}
                  </>
                ) : (
                  <p className="text-sm text-muted">Once you run the command, this page detects the machine automatically.</p>
                )}
              </Step>
            </ol>
          </CardBody>
        </Card>

        <aside className="min-w-0 space-y-4">
          <Card>
            <CardBody className="space-y-4">
              <div className="flex items-center gap-2.5">
                <ShieldCheck className="size-5 text-accent" aria-hidden />
                <h2 className="text-sm font-semibold text-fg">How your machine is protected</h2>
              </div>
              <ul className="space-y-3 text-sm leading-6 text-muted">
                <li className="flex gap-2.5">
                  <Terminal className="mt-1 size-4 shrink-0" aria-hidden />
                  Tasks run in rootless containers with no network of their own.
                </li>
                <li className="flex gap-2.5">
                  <ServerCog className="mt-1 size-4 shrink-0" aria-hidden />
                  Hard CPU, memory and process limits are enforced by your kernel.
                </li>
                <li className="flex gap-2.5">
                  <KeyRound className="mt-1 size-4 shrink-0" aria-hidden />
                  You can remove the machine and revoke its token at any time.
                </li>
              </ul>
            </CardBody>
          </Card>
          <p className="px-1 text-xs leading-5 text-muted">
            You can read what runs on your own hardware; that's inherent to renting it out. Only rent out a machine you're comfortable
            running other people's code on.
          </p>
        </aside>
      </div>
    </div>
  );
}
