import { describe, expect, it } from "vitest";
import { describeVersion, updateCommand, updateLabel } from "./update";

describe("updateCommand", () => {
  it("pulls the full public image name, then recreates the container with its own recorded command", () => {
    const agent = updateCommand("agent");
    expect(agent).toContain("podman pull docker.io/fattymango/lazycake-agent:latest");
    expect(agent).toContain("podman inspect lazycake-agent");
    expect(agent).toContain("Config.CreateCommand");
    expect(agent).toContain("subprocess.run");
    const gateway = updateCommand("gateway");
    expect(gateway).toContain("docker.io/fattymango/lazycake-gateway:latest");
    expect(gateway).toContain("podman inspect lazycake-gateway");
  });

  it("reads the recorded command before it removes the old container, so the name is free for the new one", () => {
    const agent = updateCommand("agent");
    const read = agent.indexOf("json.load(sys.stdin)");
    const remove = agent.indexOf('"podman","rm","-f","lazycake-agent"');
    const run = agent.indexOf("subprocess.run(c,check=True)");
    expect(read).toBeGreaterThan(-1);
    expect(remove).toBeGreaterThan(read);
    expect(run).toBeGreaterThan(remove);
    expect(updateCommand("gateway")).toContain('"podman","rm","-f","lazycake-gateway"');
  });

  it("never asks for the token or other settings again", () => {
    expect(updateCommand("agent")).not.toMatch(/LAZYCAKE_TOKEN|-e /);
  });
});

describe("updateLabel", () => {
  it("says something only when there is something to do", () => {
    expect(updateLabel("current")).toBeNull();
    expect(updateLabel("outdated")).toBe("Update available");
    expect(updateLabel("unknown")).toBe("Version unknown");
  });
  it("describes an unreported version in words", () => {
    expect(describeVersion("")).toBe("not reported");
    expect(describeVersion("v0.3.13")).toBe("v0.3.13");
  });
});
