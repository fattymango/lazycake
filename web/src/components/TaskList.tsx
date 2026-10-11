import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { money, parseImage, relativeTime } from "@/lib/format";
import { useNow } from "@/lib/hooks/useNow";
import type { Task } from "@/lib/types";
import { Identifier, Truncate } from "@/ui/Identifier";
import { Skeleton } from "@/ui/Skeleton";
import { TaskStatusPill } from "@/ui/StatusPill";
import { isTerminalState } from "@/ui/status";

/**
 * A narrow version of the tasks table for a side rail: one row per task (id and state, then the image, the age and the charge),
 * so it never needs the width that the table's columns do.
 */
export function TaskList({
  tasks,
  costs,
  loading,
  skeletonRows = 5,
  empty,
}: {
  tasks: Task[];
  /** Per-task charge in micro-dollars; shown on finished tasks when given. */
  costs?: Map<string, number>;
  loading?: boolean;
  skeletonRows?: number;
  empty?: ReactNode;
}) {
  const anyRunning = tasks.some((t) => !isTerminalState(t.state));
  const now = useNow(1000, anyRunning);

  if (loading) {
    return (
      <ul className="divide-y divide-border" aria-busy="true">
        {Array.from({ length: skeletonRows }, (_, i) => (
          <li key={i} className="space-y-2 px-5 py-3">
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </li>
        ))}
      </ul>
    );
  }
  if (tasks.length === 0) return <>{empty}</>;

  return (
    <ul className="divide-y divide-border">
      {tasks.map((t) => {
        const cost = costs?.get(t.id);
        return (
          <li key={t.id}>
            <Link to={`/tasks/${t.id}`} className="block min-w-0 space-y-1 px-5 py-3 transition-colors hover:bg-raised/60">
              <div className="flex items-center justify-between gap-3">
                <Identifier value={t.id} copy={false} />
                <TaskStatusPill state={t.state} size="sm" stopping={t.cancel_requested} reason={t.exit_reason} />
              </div>
              <div className="flex items-center justify-between gap-3 text-xs text-muted">
                <Truncate title={t.image}>{parseImage(t.image).name}</Truncate>
                <span className="shrink-0" data-tnum>
                  {relativeTime(t.created_at_ms, now)}
                  {cost !== undefined && cost > 0 && <> · {money(cost)}</>}
                </span>
              </div>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}
