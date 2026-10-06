import { Badge } from "./Badge";
import { connectionStatus, getTaskStatus, stoppingStatus } from "./status";
import type { StatusMeta } from "./status";

function Pill({ meta, size }: { meta: StatusMeta; size?: "sm" | "md" }) {
  return (
    <Badge tone={meta.tone} size={size} dot pulse={meta.pulse}>
      {meta.label}
    </Badge>
  );
}

/** `stopping`: the customer asked to stop it and the node hasn't confirmed yet, so show that instead of "Running". */
export function TaskStatusPill({
  state,
  size,
  stopping,
  reason,
}: {
  state: string;
  size?: "sm" | "md";
  stopping?: boolean;
  reason?: string;
}) {
  return <Pill meta={stopping ? stoppingStatus : getTaskStatus(state, reason)} size={size} />;
}

export function ConnectionPill({ connected, size }: { connected: boolean; size?: "sm" | "md" }) {
  return <Pill meta={connectionStatus(connected)} size={size} />;
}
