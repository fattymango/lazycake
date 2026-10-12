# Caveats, traps and backlog

Back to [README](README.md). Read before touching the connection, trust, billing, or deploying.

## Design limits you must know
1. **Machine identity (fixed in v0.4.4, task 8.19).** A machine is `(account, machine_id)`: the agent keeps `machine-id` in the host directory it mounts (`LAZYCAKE_LCINIT_HOST_DIR`; a bare process uses the user config dir), so restarts, updates and recreated containers are the same node and keep their trust. Gotchas: an agent that cannot write that directory registers without an id and falls back to one entry per process; two computers cloned from one disk image share an id; trust is still only in coordinator memory (a coordinator restart resets it); the first registration with an id adopts the newest offline entry of the same hostname+arch that has no id (`FindLegacyNode`), so old duplicate rows are not merged, only the newest is taken over.
2. **Trust is in memory.** Coordinator restart → everyone 0.5; the DB column is write-only.
3. **Transport encryption is half done.** The portal and MCP have HTTPS on 8080 ([https](https.md)); the agent gRPC token (7443) and the relay are still unencrypted: backlog 8.18 second half (agent TLS + a new agent release).
4. **Server hygiene:** open items about the production host are kept in the gitignored `CLAUDE.local.md` (do not write them in tracked files).
5. **Docker Hub anonymous pulls** are rate-limited per IP; a machine pulling many different images will hit it (cached digests avoid it).
6. **OOM is reported two ways** (`oom` vs `exited` 255 when the program dies on a failed allocation first) — program behaviour, left as is.
7. `Offer.bandwidth_mb_month` exists in the proto but is not implemented.

## Traps in the code
- **`conn.Runner.runOnce` must install `sendCh` before calling `OnRegistered`.** Otherwise replayed final reports are silently dropped (this bug existed for months).
- **Final reports (`TaskFinished`) are kept until a connection accepts them** (`exec.reportFinished`/`ReplayPending`). Do not re-send reports that already went out: `scheduler.OnTaskFinished` is not idempotent (trust +0.01, drift sample twice).
- **Per-connection DB work on a hot path starves the 1-vCPU coordinator** (gateway reports are batched every 5 s for that reason).
- **Agent must be the final authority on capacity**; the coordinator estimate is stale by design.
- **Spec drift and trust** punished honest machines three times during the load test; the rules in [scheduler-trust](scheduler-trust.md) (per-workload, ≥10 normalised seconds, normal exits only, relay can only prove a claim too high, healing) exist for that reason — re-run a load test if you touch them.
- **Pulls:** images are digest-pinned; a cached `@sha256:` image must not hit the registry.
- **Version strings:** `-ldflags -X` path must be the real module path (`github.com/fattymango/lazycake/...`) or binaries report an unknown version and the update indicator lies.
- **Never run a bare `go build ./cmd/x` in the repo root** (leaves a binary; root outputs are gitignored now). A 28 MB binary was once committed and had to be purged from history.
- **`internal/coordinator/webassets/dist` is gitignored**: a coordinator built without first building the web app serves no UI.
- Browser-side: Secure cookies/clipboard need fallbacks on plain HTTP.
- Wire compatibility: agents/gateways in the field lag the coordinator; every proto/behaviour change must work with both old and new peers.

## Environment quirks
- The test VM hangs when idle and must be restarted by the owner; containers there come back by themselves.
- Rootless podman only; the agent container needs `--pid=host --cap-add=SYS_ADMIN` and its host's podman socket.
- WSL2 kernels don't enforce tmpfs memory limits (historical reason the test VM exists).

## Open follow-ups (from the 2026-10 sessions)
8.18 (HTTPS), persisted trust, other pages could use the wide layout. Backlog lives in `docs/02-frontend-overhaul/IMPLEMENTATION.md` (tasks 8.12–8.19) and `PROGRESS.md`.
