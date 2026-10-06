import { ProgressBar } from "@/ui/ProgressBar";
import { Tooltip } from "@/ui/Tooltip";
import type { Tone } from "@/ui/tone";

/** 0..1 trust score as a labelled meter. New machines start at 0.50 and earn their way up. */
export function trustTone(score: number): Tone {
  return score >= 0.8 ? "success" : score >= 0.5 ? "warning" : "danger";
}

export function TrustMeter({ score }: { score: number }) {
  return (
    <Tooltip content="Trust score. It rises as a machine reliably finishes work and reports honest benchmarks, and falls when it doesn't. Higher scores are scheduled first.">
      <div className="flex min-w-0 items-center gap-2.5">
        <ProgressBar value={score} tone={trustTone(score)} label="Trust score" className="flex-1" />
        <span className="w-9 shrink-0 text-right text-xs font-medium text-fg" data-tnum>
          {score.toFixed(2)}
        </span>
      </div>
    </Tooltip>
  );
}
