import { describe, expect, it } from "vitest";
import { exitReason, getTaskStatus, isTerminalState, stoppingStatus } from "./status";

describe("exitReason for a customer's stop", () => {
  it("says nothing was charged when the task never started", () => {
    const r = exitReason("stopped", -1, false);
    expect(r?.title).toBe("Stopped by you");
    expect(r?.detail).toMatch(/nothing was charged/);
  });
  it("says only the time it ran was charged when it had started", () => {
    expect(exitReason("stopped", -1, true)?.detail).toMatch(/only for the time it ran/);
  });
  it("is not presented as a failure", () => {
    expect(exitReason("stopped", -1, true)?.tone).toBe("neutral");
  });
});

describe("stop-related status", () => {
  it("a stop is a terminal state, and a pending stop reads as Stopping", () => {
    expect(isTerminalState("cancelled")).toBe(true);
    expect(getTaskStatus("cancelled").label).toBe("Cancelled");
    expect(stoppingStatus.label).toBe("Stopping");
    expect(stoppingStatus.pulse).toBe(true);
  });
  it("a task you stopped yourself reads Stopped, a coordinator cancel stays Cancelled", () => {
    expect(getTaskStatus("cancelled", "stopped").label).toBe("Stopped");
    expect(getTaskStatus("cancelled", "cancelled").label).toBe("Cancelled");
    expect(getTaskStatus("cancelled").label).toBe("Cancelled");
    expect(getTaskStatus("failed", "stopped").label).toBe("Failed");
  });
});
