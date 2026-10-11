import { apiGet } from "@/lib/api";
import type { TaskSummary } from "@/lib/types";
import { useAsync } from "./useAsync";
import { useInterval } from "./useInterval";
import { useLiveReload } from "./useLiveReload";

/**
 * Counts over all of the account's tasks. Pages show these instead of counting the task list, which is only the newest 200: under a
 * backlog of queued tasks that list held no running or finished task at all, and the Overview read "0 running, nothing finished".
 */
export function useTaskSummary() {
  const summary = useAsync((s) => apiGet<TaskSummary>("/api/portal/customer/tasks/summary", s), []);
  useLiveReload(
    () => summary.reload(),
    (e) => e.type === "task_state"
  );
  useInterval(() => summary.reload(), 15_000);
  return summary;
}
