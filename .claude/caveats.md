# Caveats, traps and backlog

Back to [README](README.md). Read before touching the connection, trust, billing, or deploying.

## Design limits you must know
1. **Machine identity = agent process instance.** `instance_id` is regenerated at every agent start, so every restart/reboot/update creates a new node row (production had 20 for 2 machines), leaves an offline ghost, and **resets that machine's trust to 0.5 — a banned machine can shed its ban by restarting.** Backlog **8.19** (`docs/02-frontend-overhaul/IMPLEMENTATION.md`); unscheduled, owner to decide.
2. **Trust is in memory.** Coordinator restart → everyone 0.5; the DB column is write-only.
3. **No HTTPS.** Portal on plain HTTP; agent gRPC token unencrypted. Backlog 8.18 (Let's Encrypt for an IP is an idea, unverified; needs a reverse proxy and agent TLS + a new agent release).
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
Task 8.19 (identity), 8.18 (HTTPS), persisted trust, other pages could use the wide layout. Backlog lives in `docs/02-frontend-overhaul/IMPLEMENTATION.md` (tasks 8.12–8.19) and `PROGRESS.md`.
