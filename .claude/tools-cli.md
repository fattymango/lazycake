# Tools and helper programs

Back to [README](README.md).

| Program | Purpose | Notes |
|---|---|---|
| `cmd/lcctl` | customer CLI against the gRPC `CustomerService`: `submit` (incl. `--count` for fleets), `status`, `logs`, `cancel`, `nodes`, `gateway` | files `submit.go status.go logs.go cancel.go nodes.go gateway.go client.go`; talks gRPC with a customer bearer token |
| `cmd/lcinit` | static PID-1 wrapper bind-mounted into every task container (`LAZYCAKE_LCINIT_PATH` on the agent): runs the real entrypoint as a child, enforces the wall timeout with a hard exit **from inside the container** (works even if the agent and host die), forwards signals | Linux/other signal files; built statically (`CGO_ENABLED=0`) in `deploy/Dockerfile` |
| `cmd/lcbench` | public benchmark workload: burns CPU on N workers and allocates memory stepwise, prints cgroup-observed use each second — proves a node really caps a task | env `LCBENCH_DURATION`, `LCBENCH_WORKERS`, `LCBENCH_MEM_STEP_MB`, `LCBENCH_MEM_MAX_MB`; default is also the image's entrypoint |
| `cmd/refworkload` | reference workload (Monte Carlo option pricing; CPU-bound, tiny inputs, tolerates being killed); result is one JSON line on stdout | params `LAZYCAKE_MC_*`; seed varies per task |
| `tools/loadtest/loadtest.py` | production load test through the customer portal API: mixed sizes, benchmarks, network tasks through two gateways, deliberate failures, cancellations, bursts up to 300 in flight, with safety stops (coordinator unresponsive, server disk < 700 MB, balance < $50, queue stalled 5 min) | see `tools/loadtest/README.md` and `docs/LOAD_TEST_2026-10-07.md`; needs env `LAZYCAKE_BASE/USER/PASSWORD/GW_SMALL/GW_BIG[/SSH]`; never print the password |
| `web/e2e/*.mjs` | puppeteer-core browser checks | see [testing](testing.md) |
| `web/e2e/seed.py` | realistic demo data for a **demo** database (destructive) | never run against production |
| `deploy/chaos-demo.sh`, `deploy/docker-compose.yml` | local demo/compose stack (Postgres, migrate, coordinator, agents) | compose uses the weak demo DB password; dev only |
| `testdata/netns-spike.sh` | manual spike for the netns proxy | historical |

The built-in images: `deploy/Dockerfile` has stages `coordinator`, `agent` (agent + `lcinit` next to it, which the agent discovers), `gateway`, `lcctl`, `refworkload` and `lcbench`; each `go build` there stamps `internal/version.Version` from `--build-arg VERSION`.
