import { describe, expect, it } from "vitest";
import { GATEWAY_IMAGE, gatewayInstallCommand } from "./gatewayInstall";

const gw = {
  id: "gw_123",
  label: "prod",
  connected: false,
  created_at_ms: 0,
  traffic: { received_from_tasks_bytes: 0, sent_to_tasks_bytes: 0, connections: 0 },
  services: [
    { name: "db", port: 5432 },
    { name: "cache", port: 6379 },
  ],
  install_token: "tok_abc",
};

// The command is pasted by customers, so what each real failure needed must stay in it.
describe("gatewayInstallCommand", () => {
  const cmd = gatewayInstallCommand(gw, "coord.example.com");

  it("is always a container, from the full public image name", () => {
    expect(cmd).toContain("podman run -d --replace --name lazycake-gateway");
    expect(cmd).toContain(GATEWAY_IMAGE);
    expect(GATEWAY_IMAGE).toMatch(/^docker\.io\/fattymango\/lazycake-gateway:/);
    expect(cmd).not.toContain("./gateway");
  });

  it("uses host networking so it reaches the services on the machine's localhost", () => {
    expect(cmd).toContain("--network=host");
  });

  it("carries the identity, token, services and both coordinator ports", () => {
    expect(cmd).toContain("LAZYCAKE_GATEWAY_ID=gw_123");
    expect(cmd).toContain("LAZYCAKE_TOKEN=tok_abc");
    expect(cmd).toContain("LAZYCAKE_SERVICES=db:5432,cache:6379");
    expect(cmd).toContain("LAZYCAKE_COORDINATOR_ADDR=coord.example.com:7444");
    expect(cmd).toContain("LAZYCAKE_GRPC_ADDR=coord.example.com:7443");
  });

  it("keeps its key across restarts and comes back after a reboot", () => {
    expect(cmd).toContain("-v lazycake-gateway-data:/data");
    expect(cmd).toContain("LAZYCAKE_GATEWAY_KEY_PATH=/data/noise.key");
    expect(cmd).toContain("--restart=always");
    expect(cmd).toContain("podman-restart.service");
  });
});
