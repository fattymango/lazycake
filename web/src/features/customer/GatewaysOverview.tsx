import { Link } from "react-router-dom";
import { ArrowDownToLine, ArrowUpFromLine, Plug, Layers, Network } from "lucide-react";
import { bytes, count } from "@/lib/format";
import { gatewayTotals } from "@/lib/gatewayTotals";
import type { Gateway, Task } from "@/lib/types";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { RingGauge } from "@/ui/RingGauge";
import { Truncate } from "@/ui/Identifier";

/** What the gateways page opens with: how much moved through them and which carry it, and how many are up. */
export function GatewaysOverview({ gateways, tasks }: { gateways: Gateway[]; tasks: Task[] | undefined }) {
  const t = gatewayTotals(gateways, tasks ?? []);
  const max = t.ranked[0]?.total ?? 0;
  const withTraffic = t.ranked.filter((r) => r.total > 0);
  const top = withTraffic.slice(0, 6);
  const idle = t.ranked.length - withTraffic.length;
  const allUp = t.connected === gateways.length;

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <Card>
        <CardHeader title="Traffic through your gateways" description="All time, as counted by your own gateways." />
        <CardBody className="grid gap-6 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
          <dl className="min-w-0 space-y-4">
            <div>
              <dt className="flex items-center gap-2 text-xs font-medium text-muted">
                <ArrowDownToLine className="size-3.5 text-accent" aria-hidden />
                Received from tasks
              </dt>
              <dd className="mt-0.5 truncate text-2xl font-semibold tracking-tight text-fg" data-tnum>
                {bytes(t.received)}
              </dd>
            </div>
            <div>
              <dt className="flex items-center gap-2 text-xs font-medium text-muted">
                <ArrowUpFromLine className="size-3.5 text-success" aria-hidden />
                Sent to tasks
              </dt>
              <dd className="mt-0.5 truncate text-2xl font-semibold tracking-tight text-fg" data-tnum>
                {bytes(t.sent)}
              </dd>
            </div>
            <div>
              <dt className="flex items-center gap-2 text-xs font-medium text-muted">
                <Plug className="size-3.5" aria-hidden />
                Connections
              </dt>
              <dd className="mt-0.5 text-sm font-medium text-fg" data-tnum>
                {count(t.connections)} closed
              </dd>
            </div>
          </dl>

          <div className="min-w-0">
            <p className="mb-3 text-xs font-medium text-muted">Busiest gateways</p>
            {max === 0 ? (
              <p className="py-6 text-center text-sm text-muted">No task has moved data through a gateway yet.</p>
            ) : (
              <ul className="space-y-3">
                {top.map(({ g, total }) => {
                  const rx = g.traffic.received_from_tasks_bytes;
                  const tx = g.traffic.sent_to_tasks_bytes;
                  return (
                    <li key={g.id} className="min-w-0">
                      <div className="mb-1 flex items-baseline justify-between gap-3 text-xs">
                        <Link to={`/gateways/${g.id}`} className="min-w-0 font-medium text-fg hover:text-accent hover:underline">
                          <Truncate>{g.label}</Truncate>
                        </Link>
                        <span className="shrink-0 text-muted" data-tnum>
                          {bytes(total)}
                        </span>
                      </div>
                      <div
                        className="flex h-2 overflow-hidden rounded-full bg-raised"
                        role="img"
                        aria-label={`${g.label}: ${bytes(rx)} received from tasks, ${bytes(tx)} sent to tasks`}
                      >
                        <div className="h-full bg-accent" style={{ width: `${(rx / max) * 100}%` }} />
                        <div className="h-full bg-success" style={{ width: `${(tx / max) * 100}%` }} />
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
            {max > 0 && idle > 0 && (
              <p className="mt-3 text-xs text-muted">
                {idle} {idle === 1 ? "gateway has" : "gateways have"} not moved any data yet.
              </p>
            )}
          </div>
        </CardBody>
      </Card>

      <Card className="flex flex-col">
        <CardBody className="flex flex-1 flex-wrap items-center gap-x-8 gap-y-4">
          <RingGauge
            value={gateways.length ? t.connected / gateways.length : null}
            tone={allUp ? "success" : t.connected === 0 ? "danger" : "warning"}
            center={
              <div>
                <p className="text-2xl font-semibold leading-7 text-fg" data-tnum>
                  {t.connected} / {gateways.length}
                </p>
                <p className="text-2xs text-muted">connected</p>
              </div>
            }
            summary={`${t.connected} of ${gateways.length} gateways connected`}
          />
          <dl className="min-w-0 flex-1 space-y-3 text-sm">
            <div className="flex items-center justify-between gap-3">
              <dt className="flex items-center gap-2 text-muted">
                <Network className="size-4 text-accent" aria-hidden />
                Gateways
              </dt>
              <dd className="text-fg" data-tnum>
                {allUp ? "All connected" : `${gateways.length - t.connected} offline`}
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="flex items-center gap-2 text-muted">
                <Layers className="size-4" aria-hidden />
                Services published
              </dt>
              <dd className="font-semibold text-fg" data-tnum>
                {count(t.services)}
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="flex items-center gap-2 text-muted">
                <Plug className="size-4 text-success" aria-hidden />
                Tasks using them now
              </dt>
              <dd className="font-semibold text-fg" data-tnum>
                {tasks ? count(t.using) : "…"}
              </dd>
            </div>
          </dl>
        </CardBody>
      </Card>
    </div>
  );
}
