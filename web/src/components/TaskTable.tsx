import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { cpu, duration, memory, parseImage, relativeTime, shortDateTime, money } from "@/lib/format";
import { taskDurationMs } from "@/lib/tasks";
import { useNow } from "@/lib/hooks/useNow";
import type { Task } from "@/lib/types";
import { DataTable, type Column } from "@/ui/DataTable";
import { Identifier, Truncate } from "@/ui/Identifier";
import { TaskStatusPill } from "@/ui/StatusPill";
import { Tooltip } from "@/ui/Tooltip";
import { isTerminalState } from "@/ui/status";

function ImageLabel({ image }: { image: string }) {
  const { name, digest } = parseImage(image);
  return (
    <Truncate className="text-xs text-muted" title={image}>
      {name}
      {digest && <span className="font-mono text-subtle"> · {digest}</span>}
    </Truncate>
  );
}

/**
 * The tasks table used on the overview, the task list and a machine's recent
 * work. IDs shorten, images show as name + short digest, nothing can overflow.
 */
export function TaskTable({
  tasks,
  costs,
  loading,
  empty,
  showNode = true,
  getHref,
  skeletonRows,
}: {
  tasks: Task[];
  /** Per-task charge in micro-dollars; adds a Cost column when given. */
  costs?: Map<string, number>;
  loading?: boolean;
  empty?: ReactNode;
  showNode?: boolean;
  /** Where a row leads (default: /tasks/:id). Pass null for rows that don't link anywhere. */
  getHref?: ((t: Task) => string) | null;
  skeletonRows?: number;
}) {
  const navigate = useNavigate();
  const anyRunning = tasks.some((t) => !isTerminalState(t.state));
  const now = useNow(1000, anyRunning);
  const href = getHref === undefined ? (t: Task) => `/tasks/${t.id}` : getHref;

  const columns: Column<Task>[] = [
    {
      id: "task",
      header: "Task",
      width: "16rem",
      cell: (t) => (
        <div className="min-w-0 space-y-0.5">
          <Identifier value={t.id} />
          <ImageLabel image={t.image} />
        </div>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: "8.5rem",
      cell: (t) => <TaskStatusPill state={t.state} stopping={t.cancel_requested} reason={t.exit_reason} />,
    },
    {
      id: "resources",
      header: "Resources",
      width: "12rem",
      hideBelow: "md",
      cell: (t) => (
        <span className="whitespace-nowrap text-muted" data-tnum>
          {cpu(t.cores)} · {memory(t.memory_mb)}
        </span>
      ),
    },
    ...(showNode
      ? ([
          {
            id: "node",
            header: "Node",
            width: "9.5rem",
            hideBelow: "lg",
            cell: (t) => (t.node_id ? <Identifier value={t.node_id} copy={false} /> : <span className="text-subtle">Unassigned</span>),
          },
        ] as Column<Task>[])
      : []),
    {
      id: "created",
      header: "Submitted",
      width: "7.5rem",
      cell: (t) => (
        <Tooltip content={shortDateTime(t.created_at_ms)}>
          <span className="text-muted" data-tnum>
            {relativeTime(t.created_at_ms, now)}
          </span>
        </Tooltip>
      ),
    },
    {
      id: "duration",
      header: "Duration",
      width: "6rem",
      align: "right",
      hideBelow: "md",
      cell: (t) => (
        <span className="text-muted" data-tnum>
          {duration(taskDurationMs(t, now))}
        </span>
      ),
    },
    ...(costs
      ? ([
          {
            id: "cost",
            header: "Cost",
            width: "6rem",
            align: "right",
            hideBelow: "lg",
            cell: (t) => (
              <span className="text-fg" data-tnum>
                {costs.has(t.id) ? money(costs.get(t.id)) : <span className="text-subtle">—</span>}
              </span>
            ),
          },
        ] as Column<Task>[])
      : []),
  ];

  return (
    <DataTable
      caption="Tasks"
      columns={columns}
      rows={tasks}
      getRowKey={(t) => t.id}
      onRowClick={href ? (t) => navigate(href(t)) : undefined}
      loading={loading}
      skeletonRows={skeletonRows}
      empty={empty}
      minWidth={showNode ? 860 : 700}
      mobileCard={(t) => (
        <div className="min-w-0 space-y-1.5">
          <div className="flex items-center justify-between gap-3">
            <Identifier value={t.id} copy={false} />
            <TaskStatusPill state={t.state} size="sm" stopping={t.cancel_requested} reason={t.exit_reason} />
          </div>
          <ImageLabel image={t.image} />
          <p className="text-xs text-muted" data-tnum>
            {cpu(t.cores)} · {memory(t.memory_mb)} · {relativeTime(t.created_at_ms, now)}
          </p>
        </div>
      )}
    />
  );
}
