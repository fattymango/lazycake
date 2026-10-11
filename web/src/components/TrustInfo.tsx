import { useState } from "react";
import type { MouseEvent } from "react";
import { Info } from "lucide-react";
import { Popover, PopoverAnchor, PopoverContent } from "@/ui/Popover";

/** What moves a machine's trust score, as the scheduler really applies it (internal/coordinator/scheduler/trust.go). */
function TrustExplanation() {
  return (
    <div className="space-y-2.5 text-xs leading-5 text-muted">
      <p className="text-sm font-semibold text-fg">What is trust?</p>
      <p>
        A score from 0 to 1 for how much LazyCake can rely on a machine. A new machine starts at <b className="text-fg">0.50</b>. The higher
        it is, the sooner the machine is picked for work.
      </p>
      <div>
        <p className="font-medium text-fg">Goes up</p>
        <ul className="list-disc pl-4">
          <li>A task finishes cleanly: +0.01</li>
          <li>It passes a spot-check task: +0.02</li>
        </ul>
      </div>
      <div>
        <p className="font-medium text-fg">Goes down</p>
        <ul className="list-disc pl-4">
          <li>It drops a task (goes offline mid-task): −0.25</li>
          <li>It fails a spot-check task: −0.30</li>
          <li>Its reported traffic doesn't match what the gateway counted: −0.20</li>
          <li>It runs the same work much slower than its own history: −0.15</li>
        </ul>
      </div>
      <p>
        Below <b className="text-fg">0.20</b> a machine gets no work. A low score recovers on its own, about 0.20 an hour, but only back up
        to 0.50; reaching higher takes finished work. Scores restart at 0.50 when LazyCake's coordinator restarts.
      </p>
    </div>
  );
}

/**
 * A small "i" next to a trust label that opens the explanation. A button, so it works by keyboard and by tap (a hover tooltip
 * does neither). It sits inside clickable machine cards, so its clicks must not reach the card's link.
 */
export function TrustInfo() {
  const [open, setOpen] = useState(false);
  const stop = (e: MouseEvent) => e.stopPropagation();
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverAnchor asChild>
        <button
          type="button"
          aria-label="What is trust?"
          aria-expanded={open}
          onClick={(e) => {
            e.preventDefault(); // the card around it is a link
            e.stopPropagation();
            setOpen((o) => !o);
          }}
          className="inline-flex size-4 shrink-0 items-center justify-center rounded-full text-subtle transition-colors hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Info className="size-3.5" aria-hidden />
        </button>
      </PopoverAnchor>
      <PopoverContent align="start" onClick={stop}>
        <TrustExplanation />
      </PopoverContent>
    </Popover>
  );
}
