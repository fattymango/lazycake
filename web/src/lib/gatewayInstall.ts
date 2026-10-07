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
export function gatewayInstallCommand(g: CreateGatewayResponse, host: string): string {
  const services = g.services.map((s) => `${s.name}:${s.port}`).join(",");
  return [
    `podman run -d --replace --name lazycake-gateway --network=host --restart=always \\`,
    `  -v lazycake-gateway-data:/data -e LAZYCAKE_GATEWAY_KEY_PATH=/data/noise.key \\`,
    `  -e LAZYCAKE_COORDINATOR_ADDR=${host}:7444 \\`,
    `  -e LAZYCAKE_GRPC_ADDR=${host}:7443 \\`,
    `  -e LAZYCAKE_TOKEN=${g.install_token} \\`,
    `  -e LAZYCAKE_GATEWAY_ID=${g.id} \\`,
    `  -e LAZYCAKE_SERVICES=${services} \\`,
    `  ${GATEWAY_IMAGE}`,
    ``,
    `# Start it again after a reboot (rootless podman):`,
    `systemctl --user enable --now podman-restart.service && loginctl enable-linger "$USER"`,
  ].join("\n");
}
