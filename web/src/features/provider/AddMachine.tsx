import { useState } from "react";
import { Link } from "react-router-dom";
import { ArrowRight, CheckCircle2, KeyRound, Loader2, ServerCog, ShieldCheck, Terminal } from "lucide-react";
import { apiPost, errorMessage } from "@/lib/api";
import { cn } from "@/lib/cn";
import { useLiveEvents } from "@/lib/live";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import type { InstallToken } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Card, CardBody } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
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

export function AddMachine() {
  usePageTitle("Add a machine");
  const [result, setResult] = useState<InstallToken | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [connectedId, setConnectedId] = useState<string | null>(null);

  // A machine that registers after we minted a token is almost certainly this one.
  useLiveEvents((e) => {
    if (result && e.type === "node_connected") setConnectedId(e.node_id);
  });

  async function generate() {
    setBusy(true);
    setError(null);
    setConnectedId(null);
    try {
      setResult(await apiPost<InstallToken>("/api/portal/provider/nodes/install-token"));
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
                  code={"podman info --format '{{.Host.CgroupVersion}} {{.Host.Security.Rootless}}'\n# expect: v2 true"}
                />
                <p className="text-xs leading-5 text-muted">
                  Also run <span className="font-mono">loginctl enable-linger $USER</span> so the agent keeps running after you log out.
                </p>
              </Step>

              <Step n={2} title="Create an install command" done={!!result}>
                {error && <Alert tone="danger">{error}</Alert>}
                {!result ? (
                  <>
                    <p className="text-sm leading-6 text-muted">
                      This mints a token that lets one agent register as yours, and gives you the command to start it.
                    </p>
                    <Button onClick={generate} loading={busy}>
                      <KeyRound />
                      Generate install command
                    </Button>
                  </>
                ) : (
                  <>
                    <Alert tone="warning" title="Copy this now">
                      The token is shown once. Anyone with it can register a machine to your account. If it leaks or you lose it, generate a
                      new one.
                    </Alert>
                    <CodeBlock label="Run on your machine" code={result.install_command} />
                    <p className="text-xs leading-5 text-muted">
                      By default it offers 2 CPU cores, 2 GB of RAM and 8 GB of disk. Change the three{" "}
                      <span className="font-mono">LAZYCAKE_OFFER_*</span> values in the command to offer more or less.
                    </p>
                    <Button variant="ghost" size="sm" onClick={generate} loading={busy}>
                      Generate a new one
                    </Button>
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
                  <p className="flex items-center gap-2.5 text-sm text-muted">
                    <Loader2 className="size-4 animate-spin text-accent" aria-hidden />
                    Waiting for the agent to connect. This page updates by itself.
                  </p>
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
