import type { CreateGatewayResponse } from "./types";

/** The public image a gateway always runs as. A bare name resolves against docker.io/library and is denied. */
export const GATEWAY_IMAGE = "docker.io/fattymango/lazycake-gateway:latest";

/**
 * The install command for a new gateway: always a container.
 *
 * - `--network=host` so it can reach the services on the machine it runs on (they listen on that
 *   machine's localhost) and dial the coordinator the same way the machine does.
 * - A named volume for the Noise key, so the gateway keeps one identity across restarts.
 * - `--restart=always` so it comes back by itself (on a rootless engine this also needs
 *   podman-restart.service, enabled by the second line).
 */
export function gatewayInstallCommand(g: CreateGatewayResponse, host: string, maxMbps = 0): string {
  const services = g.services.map((s) => `${s.name}:${s.port}`).join(",");
  const limit = maxMbps > 0 ? [`  -e LAZYCAKE_MAX_MBPS=${maxMbps} \\`] : [];
  return [
    `podman run -d --replace --name lazycake-gateway --network=host --restart=always \\`,
    `  -v lazycake-gateway-data:/data -e LAZYCAKE_GATEWAY_KEY_PATH=/data/noise.key \\`,
    `  -e LAZYCAKE_COORDINATOR_ADDR=${host}:7444 \\`,
    `  -e LAZYCAKE_GRPC_ADDR=${host}:7443 \\`,
    `  -e LAZYCAKE_TOKEN=${g.install_token} \\`,
    `  -e LAZYCAKE_GATEWAY_ID=${g.id} \\`,
    `  -e LAZYCAKE_SERVICES=${services} \\`,
    ...limit,
    `  ${GATEWAY_IMAGE}`,
  ].join("\n");
}

/** Step 1: is podman there, does each service answer on this machine, can this machine reach the coordinator. */
export function gatewayCheckCommand(g: Pick<CreateGatewayResponse, "services">, host: string): string {
  return [
    `podman --version`,
    ...g.services.map((s) => `nc -zv 127.0.0.1 ${s.port}   # ${s.name} must say "succeeded"`),
    `nc -zv ${host} 7443   # coordinator (gRPC)`,
  ].join("\n");
}

/** Step 3: bring the container back after a reboot (rootless podman needs the timer-less restart service). */
export const GATEWAY_BOOT_COMMAND = `systemctl --user enable --now podman-restart.service && loginctl enable-linger "$USER"`;

/** Step 4: show what the gateway is doing, to confirm it connected. */
export const GATEWAY_LOGS_COMMAND = `podman logs --tail 20 lazycake-gateway`;

/** After a gateway is deleted in the dashboard its container goes idle by itself; this removes it and its key from the machine. */
export const GATEWAY_REMOVE_COMMAND = `podman rm -f lazycake-gateway && podman volume rm lazycake-gateway-data`;
