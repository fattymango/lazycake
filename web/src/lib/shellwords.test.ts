import { describe, expect, it } from "vitest";
import { hasBalancedQuotes, joinArgs, splitArgs } from "./shellwords";

describe("splitArgs", () => {
  it("splits on whitespace", () => {
    expect(splitArgs("sleep 60")).toEqual(["sleep", "60"]);
    expect(splitArgs("  a   b\tc  ")).toEqual(["a", "b", "c"]);
  });
  it("keeps quoted groups together (the old form split these wrongly)", () => {
    expect(splitArgs('sh -c "sleep 5; echo done"')).toEqual(["sh", "-c", "sleep 5; echo done"]);
    expect(splitArgs("echo 'hello world' x")).toEqual(["echo", "hello world", "x"]);
  });
  it("keeps the other quote kind literal inside a group", () => {
    expect(splitArgs(`echo "it's fine"`)).toEqual(["echo", "it's fine"]);
  });
  it("preserves an explicitly empty argument", () => {
    expect(splitArgs('cmd "" x')).toEqual(["cmd", "", "x"]);
  });
  it("returns nothing for blank input", () => {
    expect(splitArgs("")).toEqual([]);
    expect(splitArgs("   ")).toEqual([]);
  });
});

describe("hasBalancedQuotes", () => {
  it("detects an unclosed quote", () => {
    expect(hasBalancedQuotes('echo "hi')).toBe(false);
    expect(hasBalancedQuotes("echo 'hi'")).toBe(true);
    expect(hasBalancedQuotes(`echo "it's"`)).toBe(true);
  });
});

describe("joinArgs", () => {
  it("round-trips with splitArgs", () => {
    for (const argv of [
      ["sleep", "60"],
      ["sh", "-c", "sleep 5; echo done"],
      ["echo", "hello world", ""],
      ["echo", 'say "hi" now'],
    ]) {
      expect(splitArgs(joinArgs(argv))).toEqual(argv);
    }
  });
});
