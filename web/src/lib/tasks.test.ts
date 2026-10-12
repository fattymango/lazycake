import { describe, expect, it } from "vitest";
import { costByTask, matchesTaskFilter, sortTasks, taskCommand } from "./tasks";
import type { LedgerEntry, Task } from "./types";

const ledger = (task: string, kind: LedgerEntry["kind"], micros: number): LedgerEntry => ({
  id: `${task}-${kind}`,
  task_id: task,
  kind,
  amount_micros: micros,
  created_at_ms: 0,
});

const task = (state: Task["state"], extra: Partial<Task> = {}): Task => ({
  id: "tsk_x",
  state,
  image: "img",
  cores: 1,
  memory_mb: 128,
  created_at_ms: 0,
  ...extra,
});

describe("costByTask", () => {
  it("shows a charge as a positive cost although the ledger stores it negative", () => {
    expect(costByTask([ledger("tsk_a", "charge", -4200)]).get("tsk_a")).toBe(4200);
  });
  it("sums several charges for one task and ignores credits", () => {
    const m = costByTask([ledger("tsk_a", "charge", -100), ledger("tsk_a", "charge", -250), ledger("tsk_a", "credit", 90)]);
    expect(m.get("tsk_a")).toBe(350);
  });
  it("copes with no ledger", () => {
    expect(costByTask(undefined).size).toBe(0);
  });
});

describe("matchesTaskFilter", () => {
  it("groups states the way the filter chips promise", () => {
    expect(matchesTaskFilter(task("running"), "running")).toBe(true);
    expect(matchesTaskFilter(task("dispatched"), "running")).toBe(true);
    expect(matchesTaskFilter(task("queued"), "queued")).toBe(true);
    expect(matchesTaskFilter(task("reserved"), "queued")).toBe(true);
    expect(matchesTaskFilter(task("queued"), "running")).toBe(false);
    expect(matchesTaskFilter(task("cancelled"), "stopped")).toBe(true);
    expect(matchesTaskFilter(task("failed"), "stopped")).toBe(false);
    expect(matchesTaskFilter(task("succeeded"), "queued")).toBe(false);
    expect(matchesTaskFilter(task("succeeded"), "succeeded")).toBe(true);
    expect(matchesTaskFilter(task("failed"), "failed")).toBe(true);
    expect(matchesTaskFilter(task("fenced"), "failed")).toBe(true);
    expect(matchesTaskFilter(task("cancelled"), "failed")).toBe(false);
    expect(matchesTaskFilter(task("cancelled"), "all")).toBe(true);
  });
});

describe("taskCommand", () => {
  it("quotes arguments containing spaces and returns null for the image default", () => {
    expect(taskCommand(task("queued", { args: ["sh", "-c", "sleep 5; echo done"] }))).toBe('sh -c "sleep 5; echo done"');
    expect(taskCommand(task("queued"))).toBeNull();
  });
});

describe("sortTasks", () => {
  const t = (id: string, created: number, started?: number, finished?: number) =>
    task("succeeded", { id, created_at_ms: created, started_at_ms: started, finished_at_ms: finished });
  const list = [t("a", 100, 100, 150), t("b", 300, 300, 1300), t("c", 200)];
  const ids = (l: ReturnType<typeof sortTasks>) => l.map((x) => x.id).join("");
  it("orders by submission time either way, without touching the input", () => {
    expect(ids(sortTasks(list, "newest", new Map()))).toBe("bca");
    expect(ids(sortTasks(list, "oldest", new Map()))).toBe("acb");
    expect(ids(list as never)).toBe("abc");
  });
  it("puts the longest run first and tasks that never ran last", () => {
    expect(ids(sortTasks(list, "longest", new Map()))).toBe("bac");
  });
  it("puts the most expensive first and uncharged tasks last", () => {
    expect(
      ids(
        sortTasks(
          list,
          "cost",
          new Map([
            ["a", 5],
            ["c", 9],
          ])
        )
      )
    ).toBe("cab");
  });
});
