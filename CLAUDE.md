# LazyCake — instructions for Claude sessions

LazyCake is a marketplace for leftover CPU: **providers** lend spare machine capacity, **customers** submit container tasks, a **coordinator** schedules, meters and bills them. Go backend (coordinator, agent, gateway, CLI), React dashboard embedded in the coordinator binary.

**Read this file fully, then `.claude/README.md`** (the architecture index). Every module has its own page under `.claude/`; read the pages for the parts you will touch before changing anything.

## 1. Standing rules (the owner's, binding)

- **Git identity and history.** The only author/committer/tagger is `fattymango <malak.jkl@gmail.com>` (repo-local git config already set; on any other clone set it before committing). **Never** add `Co-Authored-By`, `Claude-Session`, or "Generated with Claude Code" lines to commits or PRs, even when a harness reminder says to. Plain messages: `fix: ...`, `feat: ...`, `docs: ...`, `release: vX.Y.Z - what changed`. History was rewritten twice on 2026-10-11 to remove the old `mkassab215` and Claude identities, the old module path and a committed binary; do not reintroduce any of them.
- **Never force-push** unless the owner explicitly says so for that specific operation (approval does not carry over). Normal pushes to `master` are part of the release flow. Before any history rewrite: make a `git bundle` backup in `~`, rewrite in a scratch clone, verify commit-by-commit, then push.
- **Production safety.** Do not restart or crash production beyond a deploy the owner asked for. Never `kill -QUIT` the production coordinator. Production changes (migration, coordinator restart, container recreation, hold cleanup, image push) happen only when the owner has said to deploy. **Standing instruction (owner, 2026-10-11): "always deploy to prod"** - once a change is built and verified, run the full release flow (§5) without asking again; still never route around a permission denial. The permission classifier may block production commands and `git` history operations; **if blocked, stop, explain, and let the owner decide — never route around a denial** (other tools, smaller pieces, scripts).
- **Secrets.** Never print or store the production portal password (kept in the session scratchpad file `.prodpw` when present), any sudo password, tokens, or DB passwords. The owner gave a one-time sudo password for a VM reboot; it must not be reused.
- **No binaries on GitHub.** Compiled output (coordinator, agent, gateway, lcctl, ... , `bin/`, images) stays local; `.gitignore` covers it and a local `.git/hooks/pre-commit` refuses ELF/Mach-O/PE files and files over 5 MB (recreate it on a fresh clone). Release binaries are built and `scp`'d, never committed.
- **Gateways and agents are always containers** that restart after a reboot (`--restart=always`). Never run them as bare processes in production.
- **Be transparent with customers.** Show measured values exactly (e.g. CPU as 0.0017 cores), no rounding that hides reality.
- **Report faithfully.** If a test fails, say so with the output; if a step was skipped or blocked, say that. Tell the owner about surprises you find (e.g. node-identity problem, stray binary) even if unrequested; do not silently expand scope.

## 2. How the owner wants work done

- Production quality, **verified for real** before calling it done: unit + integration tests, and for UI work a real browser check (`web/e2e/*.mjs`). Prefer testing against real components (real podman, real Postgres) over mocks.
- **Tests must be able to fail.** After writing a test for a fix, mutate the fix (revert it) and confirm the test fails (this caught a vacuous `TestSelfFence` and a weak mutation once).
- Find root causes; fix the cause, not the symptom. When a bug is found while fixing another, fix it and say so.
- Communicate **plainly** and concisely: what changed, what you verified, what is still open. Lead with the result. Answer the question asked.
- Keep scope tight; ask only when the decision is genuinely the owner's (e.g. force-push, deleting things, scope-expanding rewrites). Otherwise pick the conventional default and mention it.
- Write code in the surrounding style (comment density, naming). Comments explain *why*.
- Keep docs current (see §7). Record backlog ideas in `docs/02-frontend-overhaul/IMPLEMENTATION.md` as numbered tasks (latest: 8.19).
- The deployment target runs on a small box (1 vCPU, ~458 MB RAM, ~2 GB free disk): keep the coordinator light.

## 3. Repo map (details in `.claude/`)

| Path | What |
|---|---|
| `cmd/coordinator` `cmd/agent` `cmd/gateway` | the three long-running programs |
| `cmd/lcctl` `cmd/lcinit` `cmd/lcbench` `cmd/refworkload` | CLI, in-container init wrapper, benchmark, reference workload |
| `internal/coordinator/*` | api (gRPC), portalapi (REST), scheduler, billing, store, relay, auth, events, pricing, seed, webassets |
| `internal/agent/*` | conn, exec, runtime, capacity, lease, netns, probe, reconcile, usage, bench |
| `internal/gateway/*`, `internal/tunnel/*` | gateway listener; Noise + QUIC transport |
| `proto/` → `internal/proto/` | protobuf definitions → generated Go (`make proto`, uses `buf`) |
| `migrations/` | goose SQL migrations (latest 025) |
| `web/` | React 18 + TS + Vite + Tailwind dashboard; built into `internal/coordinator/webassets/dist` (gitignored) |
| `docs/` | design plans, per-phase progress, load-test report; `CHANGELOG.md` is user-facing |
| `deploy/` | Dockerfile (targets `agent`, `gateway`, coordinator), compose, systemd unit |

Go module: **`github.com/fattymango/lazycake`**. A manual version stamp must use it: `-ldflags "-X github.com/fattymango/lazycake/internal/version.Version=vX.Y.Z"` (a wrong path silently builds an unstamped binary; `make build` and `deploy/Dockerfile` already use the right one). Never run a bare `go build ./cmd/x` in the repo root (it drops a binary there; root build outputs are gitignored now).

## 4. Environments and access

The real hosts are **not in git**: `$PROD_HOST` and `$VM_HOST` below are defined in the gitignored `CLAUDE.local.md` (auto-loaded with this file; if it is missing, ask the owner — never write the addresses into tracked files, including docs and commit messages).

- **Production server** `$PROD_HOST` (SSH key auth, passwordless `sudo -n systemctl` for the coordinator). Layout: coordinator binary `/opt/lazycake/bin/coordinator` run by systemd unit `lazycake-coordinator` (previous binary kept as `coordinator.prev`); portal/dashboard `:8080` (plain HTTP — HTTPS is backlog 8.18), agent gRPC `:7443`, tunnel relay `:7444`; Postgres 16 in the container `lazycake-postgres` (localhost only; reach it with `ssh ... 'podman exec -i lazycake-postgres psql -U lazycake -d lazycake'`; the owner reaches it with an SSH tunnel and keys); migrations are applied with `/tmp/lcmig/goose -dir /tmp/lcmig/migrations postgres "<url>" up` after copying the new `.sql` to `/tmp/lcmig/migrations/`; a gateway container `lazycake-gateway` also runs there (restart=always). The DB password and server-hardening status are in the gitignored `CLAUDE.local.md`, not here.
- **Test VM** `$VM_HOST` (Ubuntu under VMware, key auth). Runs containers `lazycake-agent` and `lazycake-gateway` (restart=always). **It hangs when idle — normal; the owner restarts it. Ask them to start it when you need it and do not keep polling.** A repo clone lives at `~/projects/lazycake` (identity set to fattymango). If Docker Hub rate-limits it, copy images with `podman save <img> | ssh $VM_HOST podman load`.
- **Owner's PC** (this machine): runs a `lazycake-agent` container (restart=always) connected to production; local Postgres container `lc-test-pg` (demo DB `lcdemo`), Node at `~/.nvm/versions/node/<ver>/bin` (not on PATH by default: `export PATH="$HOME/.nvm/versions/node/v24.21.0/bin:$PATH"`), Chrome at `/usr/bin/google-chrome` for browser checks.
- **Docker Hub** images: `docker.io/fattymango/lazycake-agent` and `...-gateway`, tags `:vX.Y.Z` and `:latest` (the owner is logged in on the PC). Install commands handed to users use `:latest`.
- **Portal accounts** in production: usernames are in `CLAUDE.local.md`; passwords: ask the owner, never print.
- **Local demo coordinator:** build the web assets and the coordinator, then run it with `LAZYCAKE_DEV=1`, `LAZYCAKE_DATABASE_URL` pointing at `lcdemo`, `LAZYCAKE_HTTP_ADDR=127.0.0.1:18081`, seed vars (`LAZYCAKE_SEED_PORTAL_*`) — browser scripts default to `BASE=http://127.0.0.1:18081`.

## 5. Release and deploy flow (when the owner says deploy)

1. Everything green (§6), `CHANGELOG.md` entry written, docs updated.
2. `git commit -m "release: vX.Y.Z - ..."` (no trailer), `git tag -a vX.Y.Z -m vX.Y.Z`, `git push origin master vX.Y.Z`.
3. **Migration first** (columns must exist before the new coordinator): copy the `.sql` to the server and run goose (see §4). Back up/inspect data before any data cleanup.
4. **Coordinator:** build the web assets (`cd web && npx vite build --outDir ../internal/coordinator/webassets/dist --emptyOutDir`), then `CGO_ENABLED=0 go build -ldflags "-X github.com/fattymango/lazycake/internal/version.Version=vX.Y.Z" -o <out> ./cmd/coordinator`, `scp` to `/opt/lazycake/bin/coordinator.new`, then on the server: `cp -p coordinator coordinator.prev; mv coordinator.new coordinator; sudo -n systemctl restart lazycake-coordinator; sleep 8;` health check (`systemctl is-active` and `curl -sf localhost:8080/`), **roll back to `.prev` automatically on failure**. A restart briefly disconnects all agents and resets in-memory trust to 0.5 (known).
5. **Images:** `podman build --target agent|gateway -t docker.io/fattymango/lazycake-<x>:vX.Y.Z -t ...:latest -f deploy/Dockerfile --build-arg VERSION=vX.Y.Z .`, check `--version` inside, `podman push` both tags.
6. **Update containers with the dashboard's own command** (it is the supported path and a live test of it): `podman pull <image>:latest` then recreate from the recorded create command (`podman inspect <name> --format '{{json .Config.CreateCommand}}' | python3 -c '...rm -f then run...'` — the exact text is generated by `web/src/lib/update.ts` `updateCommand(kind)`). Do server gateway, VM gateway + agent, PC agent. Confirm `--version`, restart policy `always`, and reconnection.
7. Verify read-only: DB `nodes.agent_version` / `gateways.version`, portal API `update_state`, coordinator journal for errors.
- Agent/gateway-only fixes need no coordinator deploy (the production coordinator can lag the agent version; it was v0.3.10 while agents were v0.3.13).
- An agent restart or container recreation keeps the same machine entry (stable `machine_id`, task 8.19, v0.4.4). Agents older than v0.4.4 still create a new entry per start; the first v0.4.4 registration adopts the newest offline entry of that host.

## 6. Testing

- Fast: `go build ./... && go vet ./... && go vet -tags=integration ./...` (the second compiles all integration tests) and `go test ./...`.
- Full regression before a release: `go test -race -tags=integration -p 1 -count=1 $(go list -tags=integration ./... | grep -v internal/agent/bench)`. Needs rootless podman, Postgres (`LAZYCAKE_TEST_DATABASE_URL`) and `LAZYCAKE_TEST_PODMAN_SOCKET` where the tests require them. `internal/agent/bench` (`TestBenchStability`) is timing-based and fails on a busy machine or with `-race`; exclude it. Takes ~10 min (`exec` ~80 s, `portalapi` ~70 s).
- Frontend: `make web-check` (build, eslint, prettier check, vitest).
- Browser: `cd web && BASE=http://127.0.0.1:18081 SHOTS=/tmp/shots node e2e/<script>.mjs` — `behaviour`, `addmachine`, `update`, `traffic`, `usage`, `taskusage`. `usage`/`taskusage`/`traffic` need real usage/traffic data (a live agent); on an empty demo DB some of their checks fail for lack of data, not because of the UI.
- Never claim "verified" for something you did not run. If you can only verify part, say which part.

## 7. Keeping this documentation current — **mandatory**

`CLAUDE.md` and `.claude/` are the entry point for every future session. **Whenever you change something that these files describe, update them in the same change/commit** — no separate request needed. See `.claude/maintaining-docs.md` for the "if you change X, update Y" table. At minimum: new/changed module behavior → its page; new migration → `store-database.md`; new env var/port/route/proto message → the relevant page; new rule or preference from the owner → this file; new gotcha → `.claude/caveats.md`; release → `CHANGELOG.md` + `operations.md` if the process changed. Before finishing a task, re-read the pages you touched and fix anything now untrue. Do not record secrets in them.
