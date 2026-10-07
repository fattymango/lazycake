import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  ArrowRight,
  CheckCircle2,
  ChevronDown,
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
import { useLiveEvents } from "@/lib/live";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import {
  CAPACITY_COMMAND,
  FIELD_LABEL,
  FIELD_UNIT,
  HARD_LIMITS,
  OFFER_FIELDS,
  SLIDER_STOPS,
  bigOffers,
  defaultOffer,
  maxOffer,
  offerProblems,
  offerRequest,
  parseCapacity,
  parseWhole,
  stopsFor,
  type MachineCaps,
  type OfferField,
  type OfferInput,
} from "@/lib/offer";
import type { InstallToken } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
import { Input, Textarea } from "@/ui/Input";
import { PageHeader } from "@/ui/PageHeader";
import { StopSlider } from "@/ui/StopSlider";

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

const ROWS: { field: OfferField; icon: typeof Cpu; hint: string }[] = [
  { field: "cores", icon: Cpu, hint: "Cores tasks can use at the same time." },
  { field: "memoryGB", icon: MemoryStick, hint: "Shared by every task running here." },
  { field: "storageGB", icon: HardDrive, hint: "Scratch space and cached images." },
  { field: "networkMbps", icon: Gauge, hint: "Gateway traffic, all tasks together." },
];

const BIG_PHRASE: Record<OfferField, (v: number) => string> = {
  cores: (v) => `${v} CPU cores`,
  memoryGB: (v) => `${v} GB of memory`,
  storageGB: (v) => `${v} GB of storage`,
  networkMbps: (v) => `${v} Mbps of network`,
};

function CapacityRow({
  field,
  icon: Icon,
  hint,
  value,
  caps,
  error,
  onChange,
}: {
  field: OfferField;
  icon: typeof Cpu;
  hint: string;
  value: number;
  caps: MachineCaps | null;
  error?: string;
  onChange: (v: number) => void;
}) {
  const stops = SLIDER_STOPS[field];
  const sliderTop = stops[stops.length - 1];
  const unit = FIELD_UNIT[field];
  const max = maxOffer(caps);
  const knownMax = field === "networkMbps" ? caps?.networkMbps != null : !!caps;
  const beyondSlider = Number.isFinite(value) && value > sliderTop;
  // The slider never offers more than the machine has, and ends exactly at what it has.
  const sliderStops = stopsFor(field, knownMax ? max[field] : null);
  const note = error
    ? error
    : `${hint}${knownMax ? ` Machine: ${max[field].toLocaleString("en-US")} ${unit}${field === "storageGB" ? " free" : ""}.` : ""}${beyondSlider ? " Past the slider." : ""}`;
  return (
    <li className="space-y-2 rounded-xl border border-border bg-raised/30 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <label htmlFor={`offer-${field}`} className="flex items-center gap-2 text-sm font-semibold text-fg">
            <Icon className="size-4 text-muted" aria-hidden />
            {FIELD_LABEL[field]}
          </label>
          <p id={`offer-${field}-note`} className={cn("mt-1 text-xs leading-4", error ? "text-danger" : "text-muted")}>
            {note}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <Input
            id={`offer-${field}`}
            aria-label={`${FIELD_LABEL[field]} to lend`}
            inputMode="numeric"
            autoComplete="off"
            maxLength={String(HARD_LIMITS[field].max).length}
            value={Number.isFinite(value) ? String(value) : ""}
            onChange={(e) => onChange(parseWhole(e.target.value, field))}
            onKeyDown={(e) => {
              // Whole numbers only: no decimal point, sign or exponent can even be typed.
              if (["-", "+", ".", ",", "e", "E"].includes(e.key)) e.preventDefault();
            }}
            aria-invalid={!!error}
            aria-describedby={`offer-${field}-note`}
            className="h-9 w-20 text-right text-base font-semibold"
            mono
          />
          <span className="w-9 text-xs text-muted">{unit}</span>
        </div>
      </div>
      <StopSlider stops={sliderStops} value={value} onChange={onChange} label={`${FIELD_LABEL[field]} slider`} />
    </li>
  );
}

export function AddMachine() {
  usePageTitle("Add a machine");
  const [result, setResult] = useState<InstallToken | null>(null);
  const [mintedFor, setMintedFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [connectedId, setConnectedId] = useState<string | null>(null);
  const [waitedLong, setWaitedLong] = useState(false);
  const [capsText, setCapsText] = useState("");
  const [capsOpen, setCapsOpen] = useState(false);
  const caps = parseCapacity(capsText);
  const [offer, setOffer] = useState<OfferInput>(defaultOffer(null));
  const problems = offerProblems(offer, caps);
  const valid = Object.keys(problems).length === 0;
  const big = valid ? bigOffers(offer, caps) : [];
  const offerKey = JSON.stringify(offer);
  // The command was made for the limits as they were; if they changed since, it is out of date.
  const stale = !!result && mintedFor !== offerKey;

  // Telling us what the machine has pulls whatever is already chosen down to what it can give.
  function onCapsChange(text: string) {
    setCapsText(text);
    const next = parseCapacity(text);
    if (!next) return;
    const max = maxOffer(next);
    setOffer((cur) => ({
      cores: Math.min(cur.cores, max.cores),
      memoryGB: Math.min(cur.memoryGB, max.memoryGB),
      storageGB: Math.min(cur.storageGB, max.storageGB),
      networkMbps: Math.min(cur.networkMbps, max.networkMbps),
    }));
  }

  // An agent runs inside a container, where 127.0.0.1 is the container itself, so a command pointing at a
  // loopback address can never connect from another machine, or from the agent's own container.
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
      setMintedFor(offerKey);
    } catch (err) {
      setError(errorMessage(err, "Couldn't create an install token"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Add a machine" description="Choose what to lend, then run the command it generates. About two minutes." />

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        {/* Section 1: what to lend */}
        <Card>
          <CardHeader
            title="1 · What to lend"
            description="Whole numbers only. Drag to a checkpoint, or type a bigger number in the box if your machine has it."
          />
          <CardBody className="space-y-5">
            <Alert tone="warning" title="Only offer what this machine can really provide">
              If it can't provide these numbers, the agent shuts down right away and the machine fails to connect. It never shrinks your
              offer to fit.
            </Alert>

            <ul className="grid gap-4 xl:grid-cols-2" aria-label="What to lend">
              {ROWS.map((r) => (
                <CapacityRow
                  key={r.field}
                  {...r}
                  value={offer[r.field]}
                  caps={caps}
                  error={problems[r.field]}
                  onChange={(v) => setOffer((cur) => ({ ...cur, [r.field]: v }))}
                />
              ))}
            </ul>

            {big.length > 0 && (
              <Alert tone="danger" title="That's a lot of this machine">
                The agent will set aside {big.map((f) => BIG_PHRASE[f](offer[f])).join(", ")} for customers' tasks whenever they run. While
                that happens your own apps may slow down or run out of memory, and you may not have a smooth experience with this device.
                Offer less if you use it yourself.
              </Alert>
            )}

            <div className="rounded-lg border border-border bg-raised/40">
              <button
                type="button"
                onClick={() => setCapsOpen((o) => !o)}
                aria-expanded={capsOpen}
                className="flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2.5 text-left"
              >
                <span className="min-w-0">
                  <span className="block text-xs font-medium text-fg">Check against this machine's hardware (optional, recommended)</span>
                  <span className={cn("block truncate text-xs", caps ? "text-success" : "text-muted")}>
                    {caps
                      ? `${maxOffer(caps).cores} cores · ${maxOffer(caps).memoryGB} GB memory · ${maxOffer(caps).storageGB} GB free${caps.networkMbps ? ` · ${maxOffer(caps).networkMbps} Mbps` : ""}`
                      : "Limits above then match the real hardware. Skip it and the agent checks when it starts."}
                  </span>
                </span>
                <ChevronDown className={cn("size-4 shrink-0 text-muted transition-transform", capsOpen && "rotate-180")} aria-hidden />
              </button>
              {capsOpen && (
                <div className="space-y-2 border-t border-border p-3">
                  <p className="text-xs leading-5 text-muted">Run this on the machine and paste its one line of output:</p>
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
                      Got it: {maxOffer(caps).cores} cores, {maxOffer(caps).memoryGB} GB memory, {maxOffer(caps).storageGB} GB free
                      {caps.networkMbps
                        ? `, ${maxOffer(caps).networkMbps} Mbps link`
                        : ", network speed not reported (your limit is still enforced)"}
                      .
                    </p>
                  )}
                </div>
              )}
            </div>
          </CardBody>
        </Card>

        {/* Section 2: the generated steps */}
        <div className="min-w-0 space-y-4">
          <Card>
            <CardHeader title="2 · Install" description="Generated from your choices on the left." />
            <CardBody>
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

                <Step n={2} title="Create the install command" done={!!result && !stale}>
                  {error && <Alert tone="danger">{error}</Alert>}
                  <p className="text-sm leading-6 text-muted">
                    You're lending <span className="font-medium text-fg">{valid ? summary(offer) : "…"}</span>. This mints a token that lets
                    one agent register as yours.
                  </p>
                  {!result || stale ? (
                    <>
                      {stale && (
                        <Alert tone="warning" title="You changed the numbers">
                          The command below no longer matches. Generate a new one.
                        </Alert>
                      )}
                      <Button onClick={generate} loading={busy} disabled={!valid}>
                        <KeyRound />
                        Generate install command
                      </Button>
                      {!valid && <p className="text-xs text-danger">Fix the highlighted numbers on the left first.</p>}
                    </>
                  ) : null}
                  {result && (
                    <>
                      {!stale && (
                        <Alert tone="warning" title="Copy this now">
                          The token is shown once. Anyone with it can register a machine to your account. If it leaks or you lose it,
                          generate a new one.
                        </Alert>
                      )}
                      {loopback && (
                        <Alert tone="danger" title="This command points at 127.0.0.1, so it can't connect">
                          The agent runs in a container, where 127.0.0.1 is the container itself, not the machine running LazyCake. The
                          coordinator needs to advertise an address your machines can reach: set{" "}
                          <span className="font-mono">LAZYCAKE_PUBLIC_GRPC_ADDR</span> (for example{" "}
                          <span className="font-mono">203.0.113.5:7443</span>) and generate a new command.
                        </Alert>
                      )}
                      <CodeBlock label="Run on your machine" code={result.install_command} className={cn(stale && "opacity-50")} />
                      <p className="text-xs leading-5 text-muted">
                        If a number is more than the machine has, the agent stops and says which one to lower (see{" "}
                        <span className="font-mono">podman logs lazycake-agent</span>).
                      </p>
                    </>
                  )}
                </Step>

                <Step n={3} title="Wait for it to connect" done={!!connectedId} last>
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
        </div>
      </div>

      <Card>
        <CardBody className="grid gap-4 py-4 md:grid-cols-[auto_1fr_1fr_1fr] md:items-center">
          <div className="flex items-center gap-2.5">
            <ShieldCheck className="size-5 text-accent" aria-hidden />
            <h2 className="text-sm font-semibold text-fg">How your machine is protected</h2>
          </div>
          <p className="flex gap-2.5 text-xs leading-5 text-muted">
            <Terminal className="mt-0.5 size-4 shrink-0" aria-hidden />
            Tasks run in rootless containers with no network of their own.
          </p>
          <p className="flex gap-2.5 text-xs leading-5 text-muted">
            <ServerCog className="mt-0.5 size-4 shrink-0" aria-hidden />
            Hard CPU, memory and process limits are enforced by your kernel.
          </p>
          <p className="flex gap-2.5 text-xs leading-5 text-muted">
            <KeyRound className="mt-0.5 size-4 shrink-0" aria-hidden />
            You can remove the machine and revoke its token at any time. Only rent out a machine you're comfortable running other people's
            code on.
          </p>
        </CardBody>
      </Card>
    </div>
  );
}

function summary(o: OfferInput): string {
  return OFFER_FIELDS.map(
    (f) =>
      `${o[f]} ${FIELD_UNIT[f] === "cores" ? (o[f] === 1 ? "core" : "cores") : FIELD_UNIT[f]} ${f === "cores" ? "CPU" : FIELD_LABEL[f].toLowerCase()}`
  ).join(" · ");
}
