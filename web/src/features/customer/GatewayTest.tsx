import { useState } from "react";
import { Activity, CheckCircle2, HelpCircle, XCircle } from "lucide-react";
import { apiPost, errorMessage } from "@/lib/api";
import { cn } from "@/lib/cn";
import { relativeTime } from "@/lib/format";
import { useNow } from "@/lib/hooks/useNow";
import { secondsLeft } from "@/lib/throttle";
import type { GatewayServiceTest, GatewayTestResult, GatewayTestStatus } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/Popover";
import { gatewayTestStatuses } from "@/ui/status";
import { toneClasses } from "@/ui/tone";

/**
 * How long the button stays disabled after a test. This is the only throttle:
 * it lives in the frontend on purpose (one cheap probe per click from a signed-in
 * user doesn't justify server-side rate limiting yet).
 */
const COOLDOWN_MS = 10_000;

const legendOrder: GatewayTestStatus[] = ["green", "yellow", "red", "grey"];

/** The "?" next to the button: what each result colour means and what to do about it. */
function Legend() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="What do the results mean?"
          className="inline-flex size-7 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-fg data-[state=open]:bg-raised data-[state=open]:text-fg"
        >
          <HelpCircle className="size-4" aria-hidden />
        </button>
      </PopoverTrigger>
      <PopoverContent>
        <p className="text-sm font-semibold text-fg">What the results mean</p>
        <p className="mt-0.5 text-xs leading-5 text-muted">A test checks the gateway itself, then each service it publishes.</p>
        <ul className="mt-3 space-y-3">
          {legendOrder.map((k) => {
            const m = gatewayTestStatuses[k];
            return (
              <li key={k} className="flex gap-2.5">
                <m.icon className={cn("mt-0.5 size-4 shrink-0", toneClasses[m.tone].text)} aria-hidden />
                <div className="min-w-0 text-xs leading-5">
                  <p className="font-medium text-fg">{m.label}</p>
                  <p className="text-muted">{m.description}</p>
                  <p className="text-muted">{m.hint}</p>
                </div>
              </li>
            );
          })}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

function ServiceRow({ s }: { s: GatewayServiceTest }) {
  const Icon = s.ok ? CheckCircle2 : s.answered ? XCircle : HelpCircle;
  const tone = s.ok ? "text-success" : s.answered ? "text-danger" : "text-muted";
  return (
    <li className="flex min-w-0 items-start gap-2.5">
      <Icon className={cn("mt-0.5 size-4 shrink-0", tone)} aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate font-mono text-[0.8125rem] text-fg">
            {s.name}:{s.port}
          </span>
          {s.ok && s.ms !== undefined && (
            <span className="shrink-0 text-xs text-muted" data-tnum>
              {s.ms} ms
            </span>
          )}
        </div>
        <p className={cn("break-words text-xs leading-5", s.ok ? "text-muted" : s.answered ? "text-danger" : "text-muted")}>
          {s.ok ? "Reachable" : s.error || "No answer"}
        </p>
      </div>
    </li>
  );
}

function Result({ r, now }: { r: GatewayTestResult; now: number }) {
  const m = gatewayTestStatuses[r.status];
  const t = toneClasses[m.tone];
  const failing = r.services.filter((s) => !s.ok && s.answered).length;
  return (
    <div className="space-y-3" data-testid="gateway-test-result" data-status={r.status}>
      <div className={cn("flex min-w-0 items-start gap-2.5 rounded-lg border p-3", t.bg, t.border)}>
        <m.icon className={cn("mt-0.5 size-4 shrink-0", t.text)} aria-hidden />
        <div className="min-w-0 flex-1 text-xs leading-5">
          <p className="text-sm font-medium text-fg">{m.label}</p>
          <p className="text-muted">
            {r.connected && r.rtt_ms !== undefined ? `Gateway online (${r.rtt_ms} ms). ` : ""}
            {r.status === "yellow"
              ? `${failing} of ${r.services.length} ${r.services.length === 1 ? "service" : "services"} not reachable. `
              : ""}
            {m.hint}
          </p>
        </div>
      </div>
      {r.connected && r.services.length > 0 && (
        <ul className="space-y-2.5 px-0.5">
          {r.services.map((s) => (
            <ServiceRow key={s.name} s={s} />
          ))}
        </ul>
      )}
      <p className="text-2xs text-subtle">Tested {relativeTime(r.tested_at_ms, now)}</p>
    </div>
  );
}

/** "Test connection" for one gateway: button, cooldown, legend and the last result. */
export function GatewayTester({ gatewayId }: { gatewayId: string }) {
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<GatewayTestResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const now = useNow(500, running || cooldownUntil > Date.now() || result !== null);
  const left = secondsLeft(cooldownUntil, now);

  async function run() {
    setRunning(true);
    setError(null);
    try {
      setResult(await apiPost<GatewayTestResult>(`/api/portal/customer/gateways/${encodeURIComponent(gatewayId)}/test`));
    } catch (err) {
      setResult(null);
      setError(errorMessage(err, "The test couldn't run"));
    } finally {
      setRunning(false);
      setCooldownUntil(Date.now() + COOLDOWN_MS);
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-1.5">
        <Button variant="secondary" size="sm" onClick={run} loading={running} disabled={left > 0}>
          {!running && <Activity />}
          {running ? "Testing…" : left > 0 ? `Test again in ${left}s` : result || error ? "Test again" : "Test connection"}
        </Button>
        <Legend />
      </div>
      {error && (
        <Alert tone="danger" title="Couldn't run the test">
          {error}
        </Alert>
      )}
      {result && <Result r={result} now={now} />}
    </div>
  );
}
