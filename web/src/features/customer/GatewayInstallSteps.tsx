import { CheckCircle2, Loader2 } from "lucide-react";
import { cn } from "@/lib/cn";
import { GATEWAY_BOOT_COMMAND, GATEWAY_LOGS_COMMAND, gatewayCheckCommand, gatewayInstallCommand } from "@/lib/gatewayInstall";
import type { CreateGatewayResponse } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { CodeBlock } from "@/ui/CodeBlock";

function Step({ n, title, done, last, children }: { n: number; title: string; done?: boolean; last?: boolean; children: React.ReactNode }) {
  return (
    <li className="relative flex gap-3 pb-6 last:pb-0">
      {!last && <div className="absolute left-3.5 top-8 h-[calc(100%-2rem)] w-px bg-border" aria-hidden />}
      <span
        className={cn(
          "relative z-10 flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold",
          done ? "border-success/40 bg-success/15 text-success" : "border-border-strong bg-raised text-fg"
        )}
      >
        {done ? <CheckCircle2 className="size-4" aria-hidden /> : n}
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <h3 className="text-sm font-semibold text-fg">{title}</h3>
        <div className="mt-2 space-y-2">{children}</div>
      </div>
    </li>
  );
}

/** The guided install for a freshly created gateway: check, run, survive reboots, confirm it connected. */
export function GatewayInstallSteps({
  gateway,
  host,
  maxMbps,
  connected,
}: {
  gateway: CreateGatewayResponse;
  host: string;
  maxMbps: number;
  connected: boolean;
}) {
  return (
    <div className="space-y-4 pb-2">
      <Alert tone="warning" title="Copy the token now">
        It's only shown once. If you lose it, delete this gateway and create a new one.
      </Alert>
      <p className="text-xs leading-5 text-muted">
        Run these on the machine that can reach your {gateway.services.length === 1 ? "service" : "services"}. The gateway connects out to
        LazyCake, so no inbound ports are needed.
      </p>
      <ol>
        <Step n={1} title="Check the machine is ready">
          <CodeBlock label="Shell" code={gatewayCheckCommand(gateway, host)} />
          <p className="text-xs leading-5 text-muted">
            Needs podman, every service answering locally, and this machine reaching the coordinator on{" "}
            <span className="font-mono">7443</span> (gRPC) and <span className="font-mono">7444</span> (UDP, QUIC).
          </p>
        </Step>
        <Step n={2} title="Start the gateway">
          <CodeBlock label="Shell" code={gatewayInstallCommand(gateway, host, maxMbps)} />
          <p className="text-xs leading-5 text-muted">
            It runs as a container with host networking, so it reaches your services on that machine's own localhost.{" "}
            {maxMbps > 0 ? (
              <>
                The gateway itself caps all task traffic at <span className="font-semibold text-fg">{maxMbps} Mbps</span> combined.
              </>
            ) : (
              "Traffic is not capped."
            )}
          </p>
        </Step>
        <Step n={3} title="Start it again after a reboot">
          <CodeBlock label="Shell" code={GATEWAY_BOOT_COMMAND} />
          <p className="text-xs leading-5 text-muted">Rootless podman only; the container already has --restart=always.</p>
        </Step>
        <Step n={4} title="Wait for it to connect" done={connected} last>
          {connected ? (
            <p className="text-sm text-success">Connected. You can use this gateway in a task.</p>
          ) : (
            <>
              <p className="flex items-center gap-2 text-sm text-muted">
                <Loader2 className="size-4 animate-spin" aria-hidden />
                Waiting for the gateway to call in…
              </p>
              <CodeBlock label="If nothing happens" code={GATEWAY_LOGS_COMMAND} />
            </>
          )}
        </Step>
      </ol>
    </div>
  );
}
