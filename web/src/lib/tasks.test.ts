import { describe, expect, it } from "vitest";
import { costByTask, matchesTaskFilter, taskCommand } from "./tasks";
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
    expect(matchesTaskFilter(task("running"), "active")).toBe(true);
    expect(matchesTaskFilter(task("queued"), "active")).toBe(true);
    expect(matchesTaskFilter(task("succeeded"), "active")).toBe(false);
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
