import { Ban, CheckCircle2, Clock3, Ghost, Loader, ShieldAlert, Send, XCircle, Wifi, WifiOff, CircleDot } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { TaskState } from "@/lib/types";
import type { Tone } from "./tone";

// One registry for every status the UI shows. A new task state is one entry
// here; no page needs to know about it.
export interface StatusMeta {
  label: string;
  tone: Tone;
  icon: LucideIcon;
  /** Something is actively happening: animate the indicator. */
  pulse?: boolean;
  description: string;
}

export const taskStatus: Record<TaskState, StatusMeta> = {
  queued: { label: "Queued", tone: "neutral", icon: Clock3, description: "Waiting for a node with enough free capacity." },
  reserved: { label: "Reserved", tone: "info", icon: CircleDot, description: "A node has been chosen and capacity is being held." },
  dispatched: {
    label: "Starting",
    tone: "info",
    icon: Send,
    pulse: true,
    description: "Sent to a node, which is pulling the image and starting the container.",
  },
  running: { label: "Running", tone: "accent", icon: Loader, pulse: true, description: "Executing on a node." },
  succeeded: { label: "Succeeded", tone: "success", icon: CheckCircle2, description: "Finished with exit code 0." },
  failed: { label: "Failed", tone: "danger", icon: XCircle, description: "The task did not complete successfully." },
  fenced: {
    label: "Fenced",
    tone: "warning",
    icon: ShieldAlert,
    description: "The node lost contact with the coordinator, so it stopped the task to guarantee it never runs twice.",
  },
  abandoned: { label: "Abandoned", tone: "neutral", icon: Ghost, description: "No node could run this task before it was given up on." },
  cancelled: { label: "Cancelled", tone: "neutral", icon: Ban, description: "Stopped on request." },
};

export function getTaskStatus(state: string): StatusMeta {
  return taskStatus[state as TaskState] ?? { label: state, tone: "neutral", icon: CircleDot, description: "" };
}

export const isTerminalState = (state: string) =>
  state === "succeeded" || state === "failed" || state === "fenced" || state === "abandoned" || state === "cancelled";

export const isActiveState = (state: string) => !isTerminalState(state);

export const connectionStatus = (connected: boolean): StatusMeta =>
  connected
    ? { label: "Online", tone: "success", icon: Wifi, pulse: false, description: "Connected to the coordinator." }
    : { label: "Offline", tone: "neutral", icon: WifiOff, description: "Not currently connected." };

/** Why a task ended, in words a person can act on. */
export function exitReason(reason: string | undefined, exitCode: number | undefined): { title: string; detail: string; tone: Tone } | null {
  if (!reason) return null;
  switch (reason) {
    case "exited":
      return exitCode === 0
        ? { title: "Exited cleanly", detail: "The command finished with exit code 0.", tone: "success" }
        : {
            title: `Exited with code ${exitCode ?? "?"}`,
            detail: "The command ran and returned a non-zero exit code. The logs usually say why.",
            tone: "danger",
          };
    case "oom":
      return {
        title: "Out of memory",
        detail: "The task used more memory than it was allocated and was killed. Resubmit with a higher memory limit.",
        tone: "danger",
      };
    case "wall_timeout":
      return { title: "Timed out", detail: "The task ran longer than its wall-clock limit and was stopped.", tone: "danger" };
    case "error":
      return {
        title: "Could not start",
        detail: "The node was unable to create or start the container (for example, the image could not be pulled).",
        tone: "danger",
      };
    case "egress_exceeded":
      return {
        title: "Network cap exceeded",
        detail: "The task transferred more data through its gateway than its egress limit allows.",
        tone: "danger",
      };
    case "fenced":
      return {
        title: "Fenced",
        detail: "The node lost contact with the coordinator and stopped the task so it can never run twice.",
        tone: "warning",
      };
    case "abandoned":
      return { title: "Abandoned", detail: "No node picked this task up in time.", tone: "neutral" };
    default:
      return { title: reason.replace(/_/g, " "), detail: "", tone: "neutral" };
  }
}
