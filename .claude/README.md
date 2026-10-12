# LazyCake architecture reference

Start at [`/CLAUDE.md`](../CLAUDE.md) for rules, access and process; this directory explains the system.

## The system in one picture

```
 customer ──web/lcctl──► portalapi (REST) ─┐
                         api.CustomerServer ┤
                                            ▼
                    ┌──────────── COORDINATOR (one Go process) ────────────┐
                    │ store (Postgres) ◄── scheduler ◄── api (agent gRPC)  │
                    │   ▲  billing/pricing   trust       registry          │
                    │   └─ events (SSE)      relay (QUIC) ◄── gateways     │
                    └───────▲───────────────────────▲──────────────────────┘
                 gRPC stream│ (Connect)             │ QUIC+Noise relay
                            ▼                       ▼
                    AGENT (provider's machine)   GATEWAY (customer's network)
                    exec → podman containers     forwards to the customer's services
                    netns proxy ──────────────► (task reaches customer services via tunnel)
```

A **customer** submits a container task (image, limits, optional tunnel targets). The **scheduler** picks a connected **agent** with room and good trust; the agent pulls (or reuses a digest-pinned cached image), runs the container under limits, streams logs, and reports the result. **Billing** meters the task (duration normalised by the machine's benchmark), settles customer → provider through a ledger. Tasks needing the customer's private services connect through the agent's in-container proxy → QUIC+Noise tunnel relayed by the coordinator → the customer's **gateway**.

## Pages

| Page | Covers |
|---|---|
| [architecture.md](architecture.md) | end-to-end flows: task lifecycle, lease/fencing, tunnel, version/update, startup |
| [coordinator.md](coordinator.md) | `cmd/coordinator`, `api`, `auth`, `config`, `events`, `relay`, `seed`, `webassets` |
| [scheduler-trust.md](scheduler-trust.md) | placement, trust score, spec drift, canaries, reclaim, cold-pull |
| [scheduler-state.md](scheduler-state.md) | why a task is/is not placed on a machine: machine states, eligibility order, starvation hold, debugging checklist, scaling notes |
| [mcp.md](mcp.md) | MCP server and OAuth for AI clients: flow, tools, token rules, connected apps, caveats |
| [billing.md](billing.md) | meters, pricing, ledger, holds, byte reconciliation |
| [store-database.md](store-database.md) | `store` interface, Postgres tables and migrations |
| [portal-api.md](portal-api.md) | REST routes, sessions, SSE, response shapes |
| [agent.md](agent.md) | every `internal/agent/*` package and `cmd/agent` |
| [gateway-tunnel.md](gateway-tunnel.md) | `cmd/gateway`, listener, Noise, QUIC relay, netns proxy |
| [protocol.md](protocol.md) | the three `.proto` files and message semantics |
| [web-frontend.md](web-frontend.md) | React app structure, design system, conventions |
| [tools-cli.md](tools-cli.md) | `lcctl`, `lcinit`, `lcbench`, `refworkload`, load-test tool |
| [operations.md](operations.md) | build, deploy, containers, servers, troubleshooting |
| [testing.md](testing.md) | test layers, tags, environment, gotchas |
| [caveats.md](caveats.md) | known issues, traps, backlog |
| [maintaining-docs.md](maintaining-docs.md) | how to keep this directory current |

Design history (why things are the way they are) lives in `docs/` (`00-core-platform`, `01-dashboard-portals`, `02-frontend-overhaul`, `LOAD_TEST_2026-10-07.md`); `.claude/` is the *current-state* reference and links there for rationale.

## Dependency direction (keep it)

`cmd/*` wire things → `internal/*`. Inside the coordinator the seams are interfaces: `api` and `scheduler` do not import each other (scheduler defines `Dispatcher`, `api.Registry` satisfies it); `billing` depends on `api`'s event types; `pricing` is separate so `api` and `billing` can share the formula; everything uses `store.Store` (interface) and `clock.Clock`. The agent never imports `internal/coordinator`; `exec` uses `runtime.Runtime` and `capacity.Ledger` only through their types. `tunnel/*` knows nothing of coordinator code (`coordinator/relay` adapts the store to it). Preserve these seams; they are what lets tests inject fakes.
