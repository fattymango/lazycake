import { describe, expect, it } from "vitest";
import { resolveReturnPath } from "./returnPath";

describe("resolveReturnPath", () => {
  it("goes home when nothing was remembered (an explicit logout)", () => {
    expect(resolveReturnPath(undefined, "provider")).toBe("/");
    expect(resolveReturnPath(null, "customer")).toBe("/");
    expect(resolveReturnPath({}, "customer")).toBe("/");
  });
  it("returns to the page when the same role signs back in (session expired)", () => {
    expect(resolveReturnPath({ from: "/tasks/tsk_1", role: "customer" }, "customer")).toBe("/tasks/tsk_1");
  });
  it("drops the page when a different role signs in (the reported bug)", () => {
    expect(resolveReturnPath({ from: "/tasks/tsk_1", role: "customer" }, "provider")).toBe("/");
    expect(resolveReturnPath({ from: "/machines/nod_1", role: "provider" }, "customer")).toBe("/");
  });
  it("honours a deep link opened while signed out (no previous role)", () => {
    expect(resolveReturnPath({ from: "/tasks/tsk_1" }, "customer")).toBe("/tasks/tsk_1");
  });
  it("never redirects off-site or back to the auth pages", () => {
    expect(resolveReturnPath({ from: "//evil.example/x" }, "customer")).toBe("/");
    expect(resolveReturnPath({ from: "https://evil.example" }, "customer")).toBe("/");
    expect(resolveReturnPath({ from: "/login" }, "customer")).toBe("/");
    expect(resolveReturnPath({ from: "/signup" }, "customer")).toBe("/");
  });
});
