# Operations: build, deploy, servers

Back to [README](README.md). The authoritative release checklist is `CLAUDE.md` §5; this page is the detail and troubleshooting. **Only deploy when the owner says so**; if the permission classifier denies a production command, stop and ask.

## Where things run
| Where | What |
|---|---|
| `$PROD_HOST` (production, 1 vCPU / ~458 MB RAM / ~2 GB free disk) | coordinator (systemd `lazycake-coordinator`, `/opt/lazycake/bin/coordinator`, `.prev` kept), Postgres container `lazycake-postgres`, gateway container `lazycake-gateway`; goose at `/tmp/lcmig/` |
| `$VM_HOST` (test VM, hangs when idle) | agent + gateway containers; a repo clone |
| owner's PC | agent container connected to production; local test Postgres `lc-test-pg` (db `lcdemo`); where images are built and pushed |
| Docker Hub | `docker.io/fattymango/lazycake-agent`, `…-gateway` (`:vX.Y.Z`, `:latest`) |

Ports: portal/dashboard 8080 (HTTP), agent gRPC 7443, relay 7444. The server's other open ports are tracked in `CLAUDE.local.md`.

## Build
`make build` (to `bin/`), `make proto`, `make web-embed` (web → Go embed dir, then coordinator), images via `deploy/Dockerfile`. Version stamp = `-ldflags "-X github.com/fattymango/lazycake/internal/version.Version=vX.Y.Z"`; verify with `<binary> --version`. The coordinator version shown to users is what the dashboard compares agents/gateways against.

## Deploy details
- **Order:** migration → coordinator → publish images → recreate containers (server gateway, VM gateway + agent, PC agent). New columns must be backward compatible with the still-running old coordinator.
- **Coordinator swap:** scp to `coordinator.new`; `cp -p coordinator coordinator.prev; mv coordinator.new coordinator; sudo -n systemctl restart lazycake-coordinator; sleep 8;` health = `systemctl is-active` and `curl -sf localhost:8080/`; on failure move `.prev` back and restart. Expect "gRPC graceful stop timed out" in the journal; agents reconnect themselves within seconds; all trust resets to 0.5.
- **Container update (the dashboard's command):** `podman pull <img>:latest` then `podman inspect <name> --format '{{json .Config.CreateCommand}}' | python3 -c 'import json,subprocess,sys; c=json.load(sys.stdin); subprocess.run(["podman","rm","-f","<name>"],check=True); subprocess.run(c,check=True)'`. Settings, token and restart policy carry over because the create command is replayed. Works for containers created with or without `--replace`.
- **VM images:** `podman save <img> | ssh $VM_HOST podman load -q` when Docker Hub rate-limits (anonymous pulls are limited per IP).
- **Rollback:** coordinator → `.prev`; images → `podman run` the previous `:vX` tag (tags are kept); migrations are additive; down migrations exist but avoid on production.
- After deploy verify read-only: `nodes.agent_version`, `gateways.version`, portal API `update_state`, `journalctl -u lazycake-coordinator --since "5 minutes ago"`.

## Everything must survive reboot
Agents and gateways are containers with `--restart=always`; the host needs `podman-restart.service` enabled (the add-machine page says so). The coordinator and Postgres are managed by systemd/podman on the server. After the VM or PC reboots, check containers are up before assuming a bug.

## Troubleshooting
- *Queued tasks and nothing dispatches:* look for "stopped dispatching" in the coordinator log (a trust ban), agent free-capacity reports, and cooldowns after rejections.
- *Agent shows offline after laptop suspend:* needs agent ≥ v0.3.13 (ack timeout + TCP user timeout). Check `podman logs lazycake-agent` for "reconnecting".
- *Tasks stuck `running` on a live node:* the final report was lost (fixed in v0.4.0) — lease reclaim abandons it eventually.
- *Balance lower than expected:* stray `task_holds` (see [billing](billing.md)).
- *Dashboard shows many offline machines:* one entry per agent start (backlog 8.19); remove via the machine page.
- *Docker Hub 429 on a machine:* pull-through avoided by cached digests; otherwise `podman save | load`.
- Do not take a goroutine dump of production with signals (denied by the owner's safety rules).
