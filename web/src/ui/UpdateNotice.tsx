import { useState } from "react";
import { ArrowUpCircle } from "lucide-react";
import { describeVersion, updateCommand, updateLabel, type UpdateKind } from "@/lib/update";
import type { VersionInfo } from "@/lib/types";
import { Badge } from "./Badge";
import { Button } from "./Button";
import { CodeBlock } from "./CodeBlock";
import { Dialog, DialogContent } from "./Dialog";
import { Tooltip } from "./Tooltip";

/**
 * Shows that an agent or gateway is behind (or never reported its release) and opens the guided update. It never updates
 * anything itself: the machine belongs to its owner, who runs the commands.
 */
export function UpdateNotice({ kind, info, name }: { kind: UpdateKind; info: VersionInfo; name?: string }) {
  const [open, setOpen] = useState(false);
  const label = updateLabel(info.update_state);
  if (!label) return null;
  const noun = kind === "agent" ? "agent" : "gateway";
  return (
    <>
      <Tooltip
        content={
          info.update_state === "outdated"
            ? `Running ${info.version}; the newest is ${info.latest_version}. Click to see how to update.`
            : `This ${noun} is too old to report its version, so it is behind. Click to see how to update.`
        }
      >
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="rounded-full focus-visible:outline-offset-2"
          aria-label={`${label}: how to update this ${noun}`}
        >
          <Badge tone="warning" size="sm" className="cursor-pointer">
            <span className="inline-flex items-center gap-1">
              <ArrowUpCircle className="size-3 shrink-0" aria-hidden />
              {label}
            </span>
          </Badge>
        </button>
      </Tooltip>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          title={`Update ${name ?? `this ${noun}`}`}
          description={`Updating replaces the ${noun} on your own machine, so you run it yourself: LazyCake never pushes code to your hardware.`}
          className="max-w-xl"
          footer={<Button onClick={() => setOpen(false)}>Done</Button>}
        >
          <div className="space-y-4 pb-2 text-sm">
            <dl className="grid grid-cols-2 gap-3 rounded-lg border border-border bg-raised/40 p-3">
              <div>
                <dt className="text-xs text-muted">Running</dt>
                <dd className="font-mono text-fg">{describeVersion(info.version)}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted">Newest</dt>
                <dd className="font-mono text-fg">{info.latest_version}</dd>
              </div>
            </dl>
            <div className="space-y-2">
              <p className="font-medium text-fg">On the machine it runs on</p>
              <p className="text-xs leading-5 text-muted">
                This pulls the new image and recreates the container with the command it was created with, so its token and settings carry
                over.
              </p>
              <CodeBlock label="Shell" code={updateCommand(kind)} wrap />
            </div>
            <p className="text-xs leading-5 text-muted">
              Not running it as a container, or it can't find{" "}
              <span className="font-mono">{kind === "agent" ? "lazycake-agent" : "lazycake-gateway"}</span>? Run the install command again (
              {kind === "agent" ? "Add a machine" : "New gateway"}), which replaces what's running.
            </p>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
