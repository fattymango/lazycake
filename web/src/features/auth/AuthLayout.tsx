import type { ReactNode } from "react";
import { Coins, Network, ShieldCheck } from "lucide-react";
import { Logo } from "@/ui/Logo";
import { ThemeToggle } from "@/ui/ThemeToggle";

const points = [
  {
    icon: ShieldCheck,
    title: "Metering neither side can game",
    body: "Billing is normalised for hardware speed, and three independent byte counts have to agree.",
  },
  {
    icon: Network,
    title: "Your data stays yours",
    body: "Tasks reach only the gateways you installed. No SSH, no open ports, no interactive sessions.",
  },
  {
    icon: Coins,
    title: "Pay for what actually runs",
    body: "Per-second billing on digest-pinned containers, settled the moment a task finishes.",
  },
];

// A static preview of a run, so the first screen already shows what the
// product is. It is illustration, not live data.
const lineTone = { muted: "text-muted", fg: "text-fg", accent: "text-accent", success: "text-success" } as const;
const previewLines: [keyof typeof lineTone, string][] = [
  ["muted", "$ lcctl submit --cpu 0.5 --memory 512 -- ./pricer"],
  ["fg", "queued   tsk_01M4…8939F6"],
  ["accent", "running  on atlas-rack-07 · 0.50 vCPU"],
  ["fg", "t=12s  cpu_used=0.50 (limit 0.50)"],
  ["fg", "t=13s  cpu_used=0.49 (limit 0.50)"],
  ["success", "succeeded  exit 0 · $0.0021"],
];

export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="grid min-h-screen bg-bg lg:grid-cols-[minmax(0,1.05fr)_minmax(0,1fr)]">
      <aside className="relative hidden overflow-hidden border-r border-border bg-surface lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div className="bg-grid pointer-events-none absolute inset-0" aria-hidden />
        <div className="pointer-events-none absolute -left-24 -top-24 size-[28rem] rounded-full bg-accent/20 blur-[110px]" aria-hidden />
        <div className="pointer-events-none absolute -bottom-32 right-0 size-[24rem] rounded-full bg-info/10 blur-[110px]" aria-hidden />

        <Logo className="relative" />

        <div className="relative max-w-xl">
          <h1 className="text-[2.5rem] font-semibold leading-[1.1] tracking-tight text-fg">
            A marketplace for <span className="bg-gradient-to-r from-accent to-info bg-clip-text text-transparent">leftover compute</span>.
          </h1>
          <p className="mt-4 max-w-md text-base leading-7 text-muted">
            Hosts rent out spare CPU and RAM. Customers run containers on it. Everything in between is metered honestly.
          </p>

          <div className="mt-8 overflow-hidden rounded-xl border border-border bg-bg/70 shadow-pop backdrop-blur">
            <div className="flex items-center gap-1.5 border-b border-border px-4 py-2.5">
              <span className="size-2.5 rounded-full bg-danger/70" />
              <span className="size-2.5 rounded-full bg-warning/70" />
              <span className="size-2.5 rounded-full bg-success/70" />
              <span className="ml-3 text-2xs text-muted">task output</span>
            </div>
            <pre className="overflow-hidden px-4 py-3.5 font-mono text-[0.75rem] leading-6" aria-hidden>
              {previewLines.map(([tone, line], i) => (
                <div key={i} className={`truncate ${lineTone[tone]}`}>
                  {line}
                </div>
              ))}
            </pre>
          </div>
        </div>

        <ul className="relative grid gap-5 xl:grid-cols-3">
          {points.map((p) => (
            <li key={p.title} className="min-w-0">
              <p.icon className="size-5 text-accent" aria-hidden />
              <h2 className="mt-2.5 text-sm font-semibold text-fg">{p.title}</h2>
              <p className="mt-1 text-xs leading-5 text-muted">{p.body}</p>
            </li>
          ))}
        </ul>
      </aside>

      <main className="relative flex min-w-0 flex-col">
        <div className="flex items-center justify-between px-6 py-5 lg:justify-end">
          <Logo className="lg:hidden" />
          <ThemeToggle />
        </div>
        <div className="flex flex-1 items-center justify-center px-6 pb-16">
          <div className="w-full max-w-[26rem] animate-slide-up">{children}</div>
        </div>
      </main>
    </div>
  );
}
