import type { TaskState } from "../types";

export function StateBadge({ state }: { state: TaskState | string }) {
  return <span className={`state-badge state-${state}`}>{state}</span>;
}

export function ConnectedBadge({ connected }: { connected: boolean }) {
  return (
    <span
      className={`state-badge ${connected ? "text-good border-good" : "text-bad border-bad"}`}
    >
      {connected ? "connected" : "disconnected"}
    </span>
  );
}
