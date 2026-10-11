import { useMemo } from "react";
import { Activity, ArrowDownToLine, ArrowUpFromLine, Gauge } from "lucide-react";
import { bitrate } from "@/lib/bitrate";
import { shortDateTime } from "@/lib/format";
import { useGatewayRate } from "@/lib/hooks/useGatewayRate";
import type { StackPoint } from "@/lib/series";
import { Card, CardBody, CardHeader } from "@/ui/Card";
import { LineChart, type ChartSeries } from "@/ui/LineChart";
import { ProgressBar } from "@/ui/ProgressBar";
import { Skeleton } from "@/ui/Skeleton";

const SERIES: ChartSeries[] = [
  { key: "received", label: "Received from tasks", color: "rgb(var(--accent))" },
  { key: "sent", label: "Sent to tasks", color: "rgb(var(--success))" },
];

/** A small pulsing dot: this figure is being refreshed every few seconds. */
export function LiveDot() {
  return (
    <span className="relative flex size-2" aria-hidden>
      <span className="absolute inline-flex size-full animate-ping rounded-full bg-success/60" />
      <span className="relative inline-flex size-2 rounded-full bg-success" />
    </span>
  );
}

/**
 * How much a gateway is moving right now, and the last five minutes of it. The numbers are the gateway's own byte counts, averaged over
 * the last 30 s and about 10-15 s behind real time; the card says so rather than pretending to be instantaneous.
 */
export function LiveBandwidthCard({ gatewayId }: { gatewayId: string }) {
  const rate = useGatewayRate(gatewayId);
  const r = rate.data;
  const g = r?.gateways[0];
  const limitMbps = g?.max_mbps && g.max_mbps > 0 ? g.max_mbps : null;

  const points = useMemo<StackPoint[]>(
    () => (r?.series ?? []).map((p) => ({ at: p.at_ms, values: { received: p.received_from_tasks_bps, sent: p.sent_to_tasks_bps } })),
    [r]
  );
  const total = r ? r.now.received_from_tasks_bps + r.now.sent_to_tasks_bps : 0;
  const peakTotal = r ? r.peak.received_from_tasks_bps + r.peak.sent_to_tasks_bps : 0;
  const moving = total > 0;

  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            Live bandwidth <LiveDot />
          </span>
        }
        description="What this gateway is moving now, counted by the gateway itself: an average of the last 30 seconds, about 10 to 15 seconds behind."
      />
      <CardBody className="grid gap-6 lg:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
        <dl className="min-w-0 space-y-4">
          <div>
            <dt className="flex items-center gap-2 text-xs font-medium text-muted">
              <Activity className="size-3.5" aria-hidden />
              Right now
            </dt>
            <dd className="mt-0.5 truncate text-3xl font-semibold tracking-tight text-fg" data-tnum>
              {!r ? <Skeleton className="h-9 w-32" /> : moving ? bitrate(total) : "Idle"}
            </dd>
            <dd className="text-xs text-muted">{r && !moving ? "Nothing moved in the last 30 seconds" : "Both directions together"}</dd>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="min-w-0">
              <dt className="flex items-center gap-1.5 text-xs font-medium text-muted">
                <ArrowDownToLine className="size-3.5 text-accent" aria-hidden />
                From tasks
              </dt>
              <dd className="mt-0.5 truncate text-sm font-semibold text-fg" data-tnum>
                {r ? bitrate(r.now.received_from_tasks_bps) : "…"}
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="flex items-center gap-1.5 text-xs font-medium text-muted">
                <ArrowUpFromLine className="size-3.5 text-success" aria-hidden />
                To tasks
              </dt>
              <dd className="mt-0.5 truncate text-sm font-semibold text-fg" data-tnum>
                {r ? bitrate(r.now.sent_to_tasks_bps) : "…"}
              </dd>
            </div>
          </div>
          <div>
            <dt className="flex items-center gap-2 text-xs font-medium text-muted">
              <Gauge className="size-3.5" aria-hidden />
              Busiest 10 seconds, last 5 minutes
            </dt>
            <dd className="mt-0.5 text-sm font-semibold text-fg" data-tnum>
              {r ? (peakTotal > 0 ? bitrate(peakTotal) : "None") : "…"}
            </dd>
          </div>
          {limitMbps !== null && r && (
            <div className="space-y-1.5">
              <p className="flex items-baseline justify-between gap-2 text-xs">
                <span className="font-medium text-muted">Of its limit</span>
                <span className="text-fg" data-tnum>
                  {bitrate(total)} of {limitMbps} Mbps
                </span>
              </p>
              <ProgressBar
                value={total / (limitMbps * 1_000_000)}
                tone={total >= limitMbps * 900_000 ? "warning" : "accent"}
                label={`Using ${bitrate(total)} of the ${limitMbps} Mbps limit`}
              />
            </div>
          )}
        </dl>

        <div className="min-w-0">
          {!r ? (
            <Skeleton className="h-48 rounded-lg" />
          ) : (
            <LineChart
              points={points}
              series={SERIES}
              format={bitrate}
              axisFormat={bitrate}
              formatTime={(at) => shortDateTime(at)}
              axisLabel={(at) => new Date(at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}
              summary={`Bandwidth through this gateway over the last five minutes. Now ${bitrate(total)}, busiest 10 seconds ${bitrate(peakTotal)}.`}
              emptyLabel="Nothing moved in the last five minutes"
              limit={limitMbps !== null ? { value: limitMbps * 1_000_000, label: `${limitMbps} Mbps limit` } : undefined}
              showZero
              height={176}
            />
          )}
          {rate.error && !r && <p className="mt-2 text-xs text-danger">Couldn't read the live bandwidth: {rate.error}</p>}
        </div>
      </CardBody>
    </Card>
  );
}
