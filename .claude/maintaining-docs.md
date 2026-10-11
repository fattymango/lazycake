# Keeping `CLAUDE.md` and `.claude/` current

Back to [README](README.md). **This is part of every task**, not a favour: the next session reads these files instead of the conversation. Update them in the same commit as the change.

## If you change… update…
| Change | Update |
|---|---|
| a rule, preference, access detail, deploy/test step from the owner | `CLAUDE.md` (and `operations.md`/`testing.md` for detail) |
| coordinator behavior, RPC handling, config/env/port | [coordinator.md](coordinator.md) |
| placement, trust, drift, canary, reclaim numbers | [scheduler-trust.md](scheduler-trust.md) |
| pricing, holds, settlement, reconciliation | [billing.md](billing.md) |
| `Store` interface, table, **new migration** | [store-database.md](store-database.md) (migration list + notes) |
| REST route, session, response field | [portal-api.md](portal-api.md) |
| agent package behavior, agent env, install command | [agent.md](agent.md) |
| gateway, tunnel, netns, Noise/QUIC | [gateway-tunnel.md](gateway-tunnel.md) |
| `.proto` | [protocol.md](protocol.md) |
| frontend structure, conventions, update flow | [web-frontend.md](web-frontend.md) |
| CLI/helper/tool | [tools-cli.md](tools-cli.md) |
| a flow that crosses modules | [architecture.md](architecture.md) |
| new bug class, trap, limitation, backlog item | [caveats.md](caveats.md) (+ numbered task in `docs/02-frontend-overhaul/IMPLEMENTATION.md`) |
| release | `CHANGELOG.md`; process change → `CLAUDE.md` §5 + [operations.md](operations.md) |
| new package or program | a section in the right page **and** the repo map in `CLAUDE.md` and the table in [README.md](README.md) |

## How
1. Before finishing, list what you changed and re-read the matching pages; fix every statement that is now untrue (numbers, file names, env vars, routes, states). Prefer correcting a sentence over adding a new one.
2. Verify facts against the code when you write them (grep the constant, read the function); do not copy from memory. Mark unknowns as unknown.
3. Keep pages current-state and short; rationale/history belongs in `docs/`. Link rather than duplicate.
4. Keep links relative and working (markdown links to sibling files, e.g. `agent.md`); every new page must be linked from [README.md](README.md).
5. **Never put secrets or addresses in these files** (passwords, tokens, keys, DB passwords, the portal password, server IPs/users). Hosts live in the gitignored `CLAUDE.local.md`; write `$PROD_HOST` / `$VM_HOST` in docs.
6. Record the owner's new rules in `CLAUDE.md` in their own words as soon as they are given, with the reason.
7. Commit docs with the code change (`feat: … ` includes its doc updates) or, if separate, as `docs: …`. Same identity/trailer rules as any commit.

## Quick consistency checks
```
grep -rn "mkassab" CLAUDE.md .claude          # old module path/username must not appear except where describing history
ls migrations | tail -1                         # matches the latest number in store-database.md and CLAUDE.md
grep -o 'LAZYCAKE_[A-Z_]*' internal/*/config/config.go cmd/*/*.go | sort -u   # env vars match the pages
```
