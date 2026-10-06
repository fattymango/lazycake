import { Badge } from "./Badge";
import { connectionStatus, getTaskStatus } from "./status";
import type { StatusMeta } from "./status";

function Pill({ meta, size }: { meta: StatusMeta; size?: "sm" | "md" }) {
  return (
    <Badge tone={meta.tone} size={size} dot pulse={meta.pulse}>
      {meta.label}
    </Badge>
  );
}

export function TaskStatusPill({ state, size }: { state: string; size?: "sm" | "md" }) {
  return <Pill meta={getTaskStatus(state)} size={size} />;
}

export function ConnectionPill({ connected, size }: { connected: boolean; size?: "sm" | "md" }) {
  return <Pill meta={connectionStatus(connected)} size={size} />;
}
