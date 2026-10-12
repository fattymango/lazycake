import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Layers, Plus, Search, SearchX } from "lucide-react";
import { apiGet } from "@/lib/api";
import { useAsync } from "@/lib/hooks/useAsync";
import { useLiveReload } from "@/lib/hooks/useLiveReload";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { costByTask, matchesTaskFilter, sortTasks, taskCommand, type TaskFilter, type TaskSort } from "@/lib/tasks";
import type { LedgerEntry, Task } from "@/lib/types";
import { TaskTable } from "@/components/TaskTable";
import { Button } from "@/ui/Button";
import { Card } from "@/ui/Card";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { Input, Select } from "@/ui/Input";
import { PageHeader } from "@/ui/PageHeader";
import { Pagination } from "@/ui/Pagination";
import { Segmented } from "@/ui/Segmented";

const PAGE_SIZE = 12;

export function Tasks() {
  usePageTitle("Tasks");
  const tasks = useAsync((s) => apiGet<Task[]>("/api/portal/customer/tasks", s), []);
  const ledger = useAsync((s) => apiGet<LedgerEntry[]>("/api/portal/customer/ledger", s), []);
  const [filter, setFilter] = useState<TaskFilter>("all");
  const [sort, setSort] = useState<TaskSort>("newest");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);

  useLiveReload(
    () => {
      tasks.reload();
      ledger.reload();
    },
    (e) => e.type === "task_state"
  );

  const all = useMemo(() => tasks.data ?? [], [tasks.data]);
  const costs = useMemo(() => costByTask(ledger.data), [ledger.data]);

  const counts = useMemo(
    () => ({
      all: all.length,
      queued: all.filter((t) => matchesTaskFilter(t, "queued")).length,
      running: all.filter((t) => matchesTaskFilter(t, "running")).length,
      succeeded: all.filter((t) => matchesTaskFilter(t, "succeeded")).length,
      failed: all.filter((t) => matchesTaskFilter(t, "failed")).length,
      stopped: all.filter((t) => matchesTaskFilter(t, "stopped")).length,
    }),
    [all]
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const matching = all.filter(
      (t) =>
        matchesTaskFilter(t, filter) &&
        (!q ||
          t.id.toLowerCase().includes(q) ||
          t.image.toLowerCase().includes(q) ||
          (t.node_id ?? "").toLowerCase().includes(q) ||
          (taskCommand(t) ?? "").toLowerCase().includes(q))
    );
    return sortTasks(matching, sort, costs);
  }, [all, filter, query, sort, costs]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const current = Math.min(page, pageCount);
  const visible = filtered.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE);

  const reset = () => {
    setFilter("all");
    setSort("newest");
    setQuery("");
    setPage(1);
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="Tasks"
        description="Every container you've submitted, and how it went."
        actions={
          <Button asChild>
            <Link to="/tasks/new">
              <Plus />
              New task
            </Link>
          </Button>
        }
      />

      <Card className="overflow-hidden">
        <div className="flex flex-col gap-3 border-b border-border p-4 sm:flex-row sm:items-center sm:justify-between">
          <Segmented
            label="Filter by status"
            value={filter}
            onChange={(v) => {
              setFilter(v);
              setPage(1);
            }}
            options={[
              { value: "all", label: "All", count: counts.all },
              { value: "queued", label: "Queued", count: counts.queued },
              { value: "running", label: "Running", count: counts.running },
              { value: "succeeded", label: "Succeeded", count: counts.succeeded },
              { value: "failed", label: "Failed", count: counts.failed },
              { value: "stopped", label: "Stopped", count: counts.stopped },
            ]}
          />
          <Select
            value={sort}
            onChange={(e) => {
              setSort(e.target.value as TaskSort);
              setPage(1);
            }}
            aria-label="Sort tasks"
            className="sm:w-44"
          >
            <option value="newest">Newest first</option>
            <option value="oldest">Oldest first</option>
            <option value="longest">Longest run</option>
            <option value="cost">Highest cost</option>
          </Select>
          <Input
            leading={<Search />}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(1);
            }}
            placeholder="Search by ID, image or command"
            aria-label="Search tasks"
            className="sm:w-72"
          />
        </div>

        {tasks.error && !tasks.data ? (
          <ErrorState title="Couldn't load your tasks" message={tasks.error} onRetry={tasks.reload} />
        ) : (
          <>
            <TaskTable
              tasks={visible}
              costs={costs}
              loading={tasks.loading}
              skeletonRows={6}
              empty={
                all.length === 0 ? (
                  <EmptyState
                    icon={Layers}
                    title="No tasks yet"
                    description="Submit a container and it will show up here, with live logs and what it cost."
                    action={
                      <Button asChild>
                        <Link to="/tasks/new">
                          <Plus />
                          Submit your first task
                        </Link>
                      </Button>
                    }
                  />
                ) : (
                  <EmptyState
                    icon={SearchX}
                    title="No tasks match"
                    description="Nothing fits the current filter and search."
                    action={
                      <Button variant="secondary" onClick={reset}>
                        Clear filters
                      </Button>
                    }
                  />
                )
              }
            />
            <Pagination page={current} pageSize={PAGE_SIZE} total={filtered.length} onChange={setPage} />
          </>
        )}
      </Card>
    </div>
  );
}
