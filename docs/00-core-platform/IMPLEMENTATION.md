# LazyCake — Implementation Guide

A step-by-step build plan for an autonomous coding agent. Read `PLAN.md` first for the design rationale; this document is the execution order.

---

## 0. How to use this document

**You are building in phases. Do not skip ahead.** Each phase ends in a working, demoable system. Each task inside a phase lists its goal, the files it touches, what "done" means, and a command that proves it.

**Rules for the whole build:**

1. **One task at a time.** Complete a task, run its verify command, commit, then move on. Do not batch tasks.
2. **Never mark a task done without running its verify command.** If it fails, fix it before continuing.
3. **Commit after every task** with the message `<phase>.<task>: <short description>`, e.g. `1.4: agent registers and heartbeats`.
4. **If a verify command cannot run in your environment** (no Podman, no systemd, no KVM), say so explicitly, write the code anyway, write the test, mark the task `BLOCKED: <reason>` in `PROGRESS.md`, and move on. Do not fake a passing test.
5. **Do not add dependencies** not listed in section 3 without saying why in the commit message.
6. **Do not invent features.** If something seems missing, add it to `OPEN_QUESTIONS.md` rather than building it.
7. **Keep `PROGRESS.md` updated** at the repo root: one line per completed task with the date and commit hash.

**When you are unsure about a design choice**, prefer the simpler option and note the alternative in `OPEN_QUESTIONS.md`. This is a portfolio project. A working simple thing beats a half-built sophisticated thing.

---

## 1. What is being built

Three Go binaries:

- **coordinator** — central server. Owns Postgres. Assigns tasks to agents. Relays encrypted tunnel traffic.
- **agent** — runs on a host's machine. Executes containers under rootless Podman. Reports capacity.
- **gateway** — runs on a customer's server. Terminates the tunnel. Forwards to local services.

Plus a small **CLI** for submitting tasks and a **dashboard** served by the coordinator.

The core loop: a customer submits a task; the coordinator finds a node with free capacity; the agent pulls the image and runs it in a container with no network except a tunnel to the customer's gateway; the agent reports the exit code; the coordinator bills the customer and credits the host.

---

## 2. Non-goals

Do not build any of these. They are explicitly out of scope and building them is a failure, not initiative.

- GPU support of any kind.
- Object storage, blob staging, input/output declarations. Data moves through the tunnel only.
- Job fan-out, parameter sweeps, DAGs, workflow dependencies. Tasks are flat and independent.
- Any blockchain, token, or cryptocurrency.
- Kubernetes, Nomad, or any external orchestrator.
- A message broker (Kafka, RabbitMQ, NATS). Postgres is the queue.
- Real payment rails. A credits ledger table is the whole billing system.
- User signup, OAuth, password reset, email. Bearer tokens in a table.
- macOS or Windows agent support. Linux only.
- Multi-region, sharding, or horizontal scaling beyond "the coordinator can run two replicas."
- Result verification by replication. The tunnel design makes it impossible; do not attempt it.
- Any attempt to hide the workload from the host. See the threat model in `PLAN.md`.

---

## 3. Repo layout and dependencies

```
lazycake/
  cmd/
    coordinator/main.go
    agent/main.go
    gateway/main.go
    lcctl/main.go              # customer CLI
  internal/
    coordinator/
      api/                     # gRPC service impl
      scheduler/               # placement
      store/                   # Postgres access
      relay/                   # QUIC relay
      billing/
      dashboard/               # SSE + static HTML
    agent/
      runtime/                 # Podman/Docker abstraction
      capacity/                # local ledger + admission
      probe/                   # preflight capability checks
      imagecache/
      lease/                   # self-fencing
      netns/                   # namespace + proxy setup
      bench/
    gateway/
      listener/
      forward/
    tunnel/                    # shared QUIC + Noise
    proto/                     # generated code
  proto/
    lazycake/v1/*.proto
  migrations/                  # goose SQL files
  deploy/
    docker-compose.yml         # coordinator + postgres + demo agents + gateway
    systemd/                   # agent user units
  testdata/
  PROGRESS.md
  OPEN_QUESTIONS.md
  README.md
```

**Dependencies. Use these exact ones.**

| Purpose | Module |
| --- | --- |
| Postgres driver | `github.com/jackc/pgx/v5` |
| Migrations | `github.com/pressly/goose/v3` |
| gRPC | `google.golang.org/grpc` |
| Proto codegen | `buf` (CLI), `google.golang.org/protobuf` |
| QUIC | `github.com/quic-go/quic-go` |
| Noise | `github.com/flynn/noise` |
| Container runtime | `github.com/docker/docker/client` |
| Metrics | `github.com/prometheus/client_golang` |
| CLI flags | `github.com/spf13/cobra` |
| Integration test infra | `github.com/testcontainers/testcontainers-go` |
| Assertions | `github.com/stretchr/testify` |

Logging is `log/slog` from the standard library. No logrus, no zap.

---

## 4. Conventions

**Errors.** Wrap with `fmt.Errorf("doing thing: %w", err)`. Never `panic` outside `main`. Never swallow an error silently; if it is genuinely ignorable, write `_ = f()` with a comment saying why.

**Context.** Every function that does I/O takes `ctx context.Context` as its first parameter. Every goroutine has a way to be cancelled.

**Logging.** `slog` with structured fields, never string interpolation. Always include `task_id` and `node_id` where they exist:

```go
slog.Info("task dispatched", "task_id", t.ID, "node_id", n.ID, "image", t.Image)
```

**IDs.** Prefixed and human-readable: `tsk_<26 char ULID>`, `nod_`, `gw_`, `act_` (account). Generate with `github.com/oklog/ulid/v2`.

**Time.** Store UTC in Postgres as `timestamptz`. Use `time.Now()` for timestamps but `time.Since(monotonicStart)` for any deadline or duration measurement. Never compare wall-clock times across machines.

**Naming.** Packages are singular nouns (`scheduler`, not `schedulers`). Interfaces are what they do (`Runtime`, `Store`), implementations are what they are (`PodmanRuntime`, `PostgresStore`).

**Testing.** Table-driven unit tests for pure logic. Integration tests behind a `//go:build integration` tag that spin real Postgres via testcontainers. Every task's verify command must be runnable.

**Config.** Environment variables with a `LAZYCAKE_` prefix, parsed once at startup into a struct. No config files in phase 1.

**No `internal/utils`, `internal/common`, or `internal/helpers` packages.** If something does not have an obvious home, it probably belongs next to its only caller.

---

## 5. Database schema

Create these as goose migrations in `migrations/`. This is the full schema for all phases; add tables in the phase that needs them, not all at once.

```sql
-- 001_accounts.sql  (Phase 1)
CREATE TABLE accounts (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  balance_micros  BIGINT NOT NULL DEFAULT 0,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_tokens (
  token_hash  BYTEA PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  kind        TEXT NOT NULL CHECK (kind IN ('customer','agent','gateway')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at  TIMESTAMPTZ
);

-- 002_nodes.sql  (Phase 1)
CREATE TABLE nodes (
  id                TEXT PRIMARY KEY,
  account_id        TEXT NOT NULL REFERENCES accounts(id),
  hostname          TEXT NOT NULL,
  arch              TEXT NOT NULL,
  cpu_flags         TEXT[] NOT NULL DEFAULT '{}',
  capabilities      JSONB NOT NULL DEFAULT '{}',   -- probe results
  offer_cores       NUMERIC(6,2) NOT NULL DEFAULT 0,
  offer_memory_mb   INT NOT NULL DEFAULT 0,
  offer_disk_mb     INT NOT NULL DEFAULT 0,
  bench_score       NUMERIC(8,3),                  -- relative to reference = 1.000
  trust_score       NUMERIC(4,3) NOT NULL DEFAULT 0.500,
  connected         BOOLEAN NOT NULL DEFAULT false,
  last_heartbeat_at TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX nodes_available ON nodes (connected) WHERE connected;

-- 003_tasks.sql  (Phase 1)
CREATE TYPE task_state AS ENUM (
  'queued','reserved','dispatched','running',
  'succeeded','failed','fenced','abandoned','cancelled'
);

CREATE TABLE tasks (
  id                TEXT PRIMARY KEY,
  account_id        TEXT NOT NULL REFERENCES accounts(id),
  idempotency_key   TEXT,
  state             task_state NOT NULL DEFAULT 'queued',

  image             TEXT NOT NULL,           -- must contain '@sha256:'
  entrypoint        TEXT[],
  args              TEXT[],
  env               JSONB NOT NULL DEFAULT '{}',
  workdir           TEXT,

  cpu_cores         NUMERIC(6,2) NOT NULL,
  memory_mb         INT NOT NULL,
  disk_mb           INT NOT NULL,
  tmpfs_mb          INT NOT NULL DEFAULT 0,
  pids_limit        INT NOT NULL DEFAULT 256,
  wall_timeout_s    INT NOT NULL,
  no_output_timeout_s INT NOT NULL DEFAULT 0,
  egress_mb         INT NOT NULL DEFAULT 0,

  req_arch          TEXT NOT NULL DEFAULT 'amd64',
  req_cpu_flags     TEXT[] NOT NULL DEFAULT '{}',
  req_isolation     TEXT NOT NULL DEFAULT 'podman',
  req_confidentiality TEXT NOT NULL DEFAULT 'none',

  gateway_ids       TEXT[] NOT NULL DEFAULT '{}',
  delivery          TEXT NOT NULL DEFAULT 'at_most_once',
  max_attempts      INT NOT NULL DEFAULT 1,
  attempt           INT NOT NULL DEFAULT 0,

  node_id           TEXT REFERENCES nodes(id),
  lease_expires_at  TIMESTAMPTZ,
  requeue_after     TIMESTAMPTZ,

  exit_code         INT,
  exit_reason       TEXT,     -- 'exited','oom','disk','wall_timeout','no_output','fenced','abandoned'
  started_at        TIMESTAMPTZ,
  finished_at       TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX tasks_idem ON tasks (account_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;
CREATE INDEX tasks_queue ON tasks (state, created_at) WHERE state = 'queued';
CREATE INDEX tasks_reclaim ON tasks (requeue_after)
  WHERE state IN ('reserved','dispatched','running');

-- 004_logs.sql  (Phase 1)
CREATE TABLE task_logs (
  task_id   TEXT NOT NULL REFERENCES tasks(id),
  seq       BIGINT NOT NULL,
  stream    TEXT NOT NULL CHECK (stream IN ('stdout','stderr')),
  at        TIMESTAMPTZ NOT NULL,
  line      TEXT NOT NULL,
  PRIMARY KEY (task_id, seq)
);

-- 005_image_cache.sql  (Phase 1)
CREATE TABLE node_images (
  node_id    TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  digest     TEXT NOT NULL,
  size_bytes BIGINT NOT NULL,
  last_used  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (node_id, digest)
);

-- 006_gateways.sql  (Phase 2)
CREATE TABLE gateways (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id),
  label        TEXT NOT NULL,
  noise_pubkey BYTEA NOT NULL,
  services     JSONB NOT NULL DEFAULT '[]',  -- [{name, port}]
  connected    BOOLEAN NOT NULL DEFAULT false,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 007_metering.sql  (Phase 4)
CREATE TABLE task_meters (
  task_id            TEXT PRIMARY KEY REFERENCES tasks(id),
  duration_s         NUMERIC(10,3) NOT NULL,
  normalised_s       NUMERIC(10,3) NOT NULL,
  bytes_agent        BIGINT NOT NULL DEFAULT 0,
  bytes_relay        BIGINT NOT NULL DEFAULT 0,
  bytes_gateway      BIGINT NOT NULL DEFAULT 0,
  cold_pull_bytes    BIGINT NOT NULL DEFAULT 0,
  reconciled         BOOLEAN NOT NULL DEFAULT false,
  divergence_pct     NUMERIC(6,3)
);

CREATE TABLE ledger (
  id          BIGSERIAL PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  task_id     TEXT REFERENCES tasks(id),
  kind        TEXT NOT NULL,  -- 'charge','credit','cold_start','topup'
  micros      BIGINT NOT NULL,
  note        TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ledger_account ON ledger (account_id, created_at DESC);
```

**Money is `BIGINT` micros throughout. Never float.**

---

## 6. Proto definitions

`proto/lazycake/v1/agent.proto` — the agent control stream.

```protobuf
syntax = "proto3";
package lazycake.v1;
option go_package = "github.com/you/lazycake/internal/proto/lazycakev1";

service AgentService {
  // Long-lived bidirectional stream. Agent dials out through NAT.
  rpc Connect(stream AgentMessage) returns (stream CoordinatorMessage);
}

message AgentMessage {
  oneof body {
    Register      register     = 1;
    Heartbeat     heartbeat    = 2;
    CapacityReport capacity    = 3;
    CacheDelta    cache_delta  = 4;
    TaskAccepted  accepted     = 5;
    TaskRejected  rejected     = 6;
    TaskStarted   started      = 7;
    TaskFinished  finished     = 8;
    LogBatch      logs         = 9;
  }
}

message CoordinatorMessage {
  oneof body {
    RegisterAck  register_ack = 1;
    HeartbeatAck heartbeat_ack = 2;
    Dispatch     dispatch      = 3;
    Cancel       cancel        = 4;
  }
}

message Register {
  string token        = 1;
  string hostname     = 2;
  string arch         = 3;
  repeated string cpu_flags = 4;
  Capabilities caps   = 5;
  Offer offer         = 6;
  string boot_id      = 7;
  string instance_id  = 8;
  repeated CachedImage images = 9;  // full reconciliation on connect
}

message Capabilities {
  bool memory_limit = 1;
  bool cpu_quota    = 2;
  bool pids_limit   = 3;
  bool disk_limit   = 4;
  bool gvisor       = 5;
  bool systemd_slice = 6;
  string runtime    = 7;   // "podman" | "docker"
  string cgroup_version = 8;
}

message Offer {
  double cores    = 1;
  int32 memory_mb = 2;
  int32 disk_mb   = 3;
  int64 bandwidth_mb_month = 4;
  int32 image_cache_mb = 5;
}

message RegisterAck {
  string node_id      = 1;
  int32 heartbeat_s   = 2;
  int32 lease_s       = 3;   // agent fences at this many seconds without ack
  bool  benchmark_now = 4;
}

message Heartbeat {
  int64 seq = 1;
  repeated string running_task_ids = 2;  // re-announce for adoption
}

message HeartbeatAck { int64 seq = 1; }

message CapacityReport {
  Offer offer          = 1;
  double free_cores    = 2;
  int32 free_memory_mb = 3;
  int32 free_disk_mb   = 4;
  double load_avg      = 5;
  double temp_celsius  = 6;
}

message CachedImage {
  string digest     = 1;
  int64  size_bytes = 2;
}

message CacheDelta {
  repeated CachedImage pulled  = 1;
  repeated string      evicted = 2;
}

message Dispatch {
  string task_id       = 1;
  string image         = 2;   // digest-pinned
  repeated string entrypoint = 3;
  repeated string args  = 4;
  map<string,string> env = 5;
  string workdir        = 6;
  Limits limits         = 7;
  string isolation      = 8;
  repeated TunnelTarget targets = 9;
  int64 lease_expires_unix_ms = 10;
}

message Limits {
  double cpu_cores = 1;
  int32 memory_mb  = 2;
  int32 disk_mb    = 3;
  int32 tmpfs_mb   = 4;
  int32 pids       = 5;
  int32 wall_timeout_s = 6;
  int32 no_output_timeout_s = 7;
  int32 egress_mb  = 8;
}

message TunnelTarget {
  string gateway_id = 1;
  string hostname   = 2;   // what the container resolves
  int32  port       = 3;
  bytes  noise_pubkey = 4;
}

message TaskAccepted { string task_id = 1; }
message TaskRejected { string task_id = 1; string reason = 2; }
message TaskStarted  { string task_id = 1; int64 at_unix_ms = 2; }

message TaskFinished {
  string task_id     = 1;
  int32  exit_code   = 2;
  string exit_reason = 3;
  int64  at_unix_ms  = 4;
  int64  bytes_sent  = 5;
  int64  bytes_recv  = 6;
  int64  cold_pull_bytes = 7;
}

message Cancel { string task_id = 1; string reason = 2; }

message LogBatch {
  string task_id = 1;
  repeated LogLine lines = 2;
}

message LogLine {
  int64  seq    = 1;
  string stream = 2;
  int64  at_unix_ms = 3;
  string line   = 4;
}
```

---

## 7. Phase 0 — Scaffolding

**Goal: a repo that builds, tests, lints and runs migrations.**

### Task 0.1 — Repo skeleton

Create the module, directory tree from section 3, `.gitignore`, `Makefile` with targets `build`, `test`, `lint`, `migrate`, `proto`. Create empty `PROGRESS.md` and `OPEN_QUESTIONS.md`.

**Done when:** `make build` compiles three empty `main` packages that each print their name and exit 0.

**Verify:** `make build && ./bin/coordinator --version && ./bin/agent --version && ./bin/gateway --version`

### Task 0.2 — Postgres and migrations

Add `deploy/docker-compose.yml` with a Postgres 16 service. Wire goose. Write migrations 001 through 005 from section 5.

**Done when:** migrations apply cleanly to a fresh database and `goose down` reverses them.

**Verify:** `docker compose up -d postgres && make migrate && make migrate-down && make migrate`

### Task 0.3 — Proto generation

Add `buf.yaml` and `buf.gen.yaml`. Generate Go code from `proto/lazycake/v1/agent.proto` into `internal/proto/lazycakev1`.

**Done when:** generated code compiles and is committed (commit generated code; do not require contributors to run buf).

**Verify:** `make proto && go build ./...`

### Task 0.4 — Config and logging

A `Config` struct per binary, loaded from `LAZYCAKE_*` env vars, validated at startup with clear errors for missing required values. `slog` configured to JSON in production and text when `LAZYCAKE_DEV=1`.

**Done when:** each binary starts, logs its config (with secrets redacted), and exits cleanly on SIGTERM.

**Verify:** `LAZYCAKE_DEV=1 ./bin/coordinator 2>&1 | head -5`

---

## 8. Phase 1 — Vertical slice

**Goal: submit a task from the CLI, watch it run in a container on an agent, get the exit code and logs back. No tunnel, no billing, no gVisor.**

This is the phase that makes everything else possible. Do not move to Phase 2 until the demo in Task 1.10 works.

### Task 1.1 — Store layer

`internal/coordinator/store` with a `Store` interface and `PostgresStore` implementation. Methods for accounts, tokens, nodes, tasks and logs. Use `pgx` directly, no ORM. Every method takes `ctx`.

**Done when:** integration tests cover create, read, update and the state transitions for tasks.

**Verify:** `go test -tags=integration ./internal/coordinator/store/...`

### Task 1.2 — Task queue with SKIP LOCKED

Implement `ClaimQueuedTask(ctx, nodeID, filter)` that atomically selects one queued task matching a node's arch, flags, isolation and free capacity, moves it to `reserved`, sets `node_id` and `requeue_after`, and returns it. Use `SELECT ... FOR UPDATE SKIP LOCKED`.

```sql
UPDATE tasks SET state='reserved', node_id=$1, requeue_after=now() + $2::interval
WHERE id = (
  SELECT id FROM tasks
  WHERE state='queued' AND req_arch=$3 AND req_isolation=ANY($4)
    AND cpu_cores <= $5 AND memory_mb <= $6 AND disk_mb <= $7
  ORDER BY created_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
RETURNING *;
```

**Done when:** a test spawning 20 concurrent claimers against 20 queued tasks assigns each task exactly once.

**Verify:** `go test -tags=integration -race -run TestClaimConcurrent ./internal/coordinator/store/...`

### Task 1.3 — gRPC server and agent stream

Implement `AgentService.Connect`. On `Register`: authenticate the token, upsert the node row, mark connected, return `RegisterAck` with node ID and timings. Maintain an in-memory registry of connected agents keyed by node ID with a send channel each. On stream close: mark disconnected.

**Done when:** an agent can connect, register, and the coordinator logs the registration with a node ID.

**Verify:** `go test -race ./internal/coordinator/api/...`

### Task 1.4 — Agent connect, register, heartbeat

Agent dials the coordinator, registers, then heartbeats every `heartbeat_s`. Reconnect with exponential backoff and jitter (start 1s, cap 60s). On reconnect, re-register including currently running task IDs.

**Done when:** killing the coordinator and restarting it causes the agent to reconnect within 60s without operator action.

**Verify:** `go test -race ./internal/agent/...` plus a manual restart check documented in `PROGRESS.md`

### Task 1.5 — Preflight capability probe

`internal/agent/probe`. Actually test each capability rather than reading version strings:

- **cgroup version** — read `/sys/fs/cgroup/cgroup.controllers`; absent means v1, which is unsupported.
- **memory limit** — start a scratch container with `--memory=64m`, run something that allocates 128MB, confirm it is OOM-killed.
- **cpu quota** — start with `--cpus=0.5`, run a busy loop for 2s, confirm measured CPU time is near 1s not 2s.
- **pids limit** — start with `--pids-limit=16`, attempt to spawn 64 processes, confirm failure.
- **disk limit** — attempt to create and mount a sparse file; report whether it worked.
- **systemd slice** — check `Delegate=yes` on the user slice via `systemctl --user show`.
- **gvisor** — check `runsc` is on PATH and a scratch container runs under it.
- **subuid/subgid** — parse `/etc/subuid` and `/etc/subgid` for the current user; require at least 65536 IDs.

Each probe returns pass, fail, or skipped-with-reason. The agent refuses to advertise a capability that failed and logs a clear actionable message for each failure.

**Done when:** on a machine with cgroups v1 or missing subuid ranges, the agent exits with an error naming the exact fix.

**Verify:** `./bin/agent probe` prints a table of capabilities and exits non-zero if memory or cpu enforcement failed

### Task 1.6 — Runtime abstraction

`internal/agent/runtime` with:

```go
type Runtime interface {
    Pull(ctx context.Context, digest string) (sizeBytes int64, err error)
    Create(ctx context.Context, spec Spec) (containerID string, err error)
    Start(ctx context.Context, id string) error
    Wait(ctx context.Context, id string) (Result, error)
    Stop(ctx context.Context, id string, grace time.Duration) error
    Logs(ctx context.Context, id string) (io.ReadCloser, error)
    Remove(ctx context.Context, id string) error
    ListLabelled(ctx context.Context, key, value string) ([]string, error)
}
```

Implement once against `docker/docker/client`, pointed at whichever socket is available (`$XDG_RUNTIME_DIR/podman/podman.sock` first, then `/var/run/docker.sock`). Label every container `lazycake.task_id`, `lazycake.instance_id`, `lazycake.boot_id`.

`Spec` carries the limits from the proto plus `Isolation` which selects the OCI runtime (`crun` or `runsc`).

**Done when:** an integration test runs `alpine@sha256:...` with `echo hello`, gets exit 0 and the string "hello" from logs.

**Verify:** `go test -tags=integration ./internal/agent/runtime/...`

### Task 1.7 — Capacity ledger and admission control

`internal/agent/capacity`. Tracks offer, allocated and free. `Admit(spec) error` returns an error if the task does not fit. Clamp the offer: never more than 75% of physical cores, never leave less than 2GB RAM or 10GB disk free for the OS. Report capacity to the coordinator on every change and at least every 30s.

**Done when:** dispatching two tasks that individually fit but jointly exceed memory causes the second to be rejected with `TaskRejected`.

**Verify:** `go test -race ./internal/agent/capacity/...`

### Task 1.8 — Dispatch and execution

Coordinator: a placement loop that, for each connected agent with free capacity, claims a matching task and sends `Dispatch`. On `TaskRejected`, return the task to `queued` and do not re-offer it to that node for 30s.

Agent: on `Dispatch`, run admission control, reply accepted or rejected, pull the image if absent, create and start the container with `--network=none` and all limits applied, send `TaskStarted`, stream logs, wait, send `TaskFinished` with exit code and reason.

Exit reason mapping: normal exit is `exited`; cgroup OOM (exit 137 with the memory event set) is `oom`; wall timeout kill is `wall_timeout`; no output past the threshold is `no_output`.

**Done when:** a task submitted via the store reaches `succeeded` with the right exit code, and a task exceeding its memory limit reaches `failed` with reason `oom`.

**Verify:** `go test -tags=integration -run TestDispatchEndToEnd ./internal/coordinator/...`

### Task 1.9 — Log relay with caps

Agent batches log lines and sends them every 250ms or 64KB, whichever comes first. **Cap at 1MB/s and 50MB total per task.** On breach, stop relaying, insert a single line `[lazycake] log output truncated at 50MB` and continue running the task. If the coordinator is unreachable, drop lines rather than buffering unboundedly; keep at most 4MB in memory.

Coordinator writes batches to `task_logs` with the sequence number from the agent.

**Done when:** a container emitting 500MB of logs does not fill the host's disk, does not OOM the agent, and the task still completes.

**Verify:** `go test -tags=integration -run TestLogFlood ./internal/agent/...`

### Task 1.10 — CLI and the Phase 1 demo

`lcctl submit --image <digest> --cpu 1 --memory 512 --disk 1024 --timeout 60 -- <command>`, plus `lcctl status <task_id>`, `lcctl logs <task_id> [-f]`, `lcctl nodes`.

Extend `deploy/docker-compose.yml` to bring up Postgres, the coordinator, and three agents.

**Done when:** `docker compose up`, then `lcctl submit ...` shows the task moving queued → dispatched → running → succeeded, with logs retrievable, and `lcctl nodes` shows three nodes with their capacity.

**Verify:**
```bash
docker compose up -d
lcctl submit --image alpine@sha256:<digest> --cpu 1 --memory 256 --disk 512 --timeout 30 -- sh -c 'echo hi; sleep 5; echo bye'
lcctl logs <task_id>   # prints hi, bye
lcctl status <task_id> # succeeded, exit 0
```

**Record a terminal GIF of this. It is the first artefact for the README.**

---

## 9. Phase 2 — The tunnel

**Goal: a container with no network reaches exactly the customer gateways its task declared, and nothing else.**

This is the riskiest phase. Rootless network namespace manipulation often does not behave as documented. Spike Task 2.1 before committing to the rest; if it proves impossible in rootless mode, record that in `OPEN_QUESTIONS.md` and fall back to a userspace-only design where the container talks to a local listener on its loopback.

### Task 2.1 — Netns spike

Before writing production code, prove the mechanism in a throwaway script:

1. Create a container with `--network=none`.
2. From the agent (unprivileged), enter that container's network namespace.
3. Bring up loopback and bind a listener inside it.
4. Confirm a process inside the container can connect to that listener.
5. Confirm the container cannot reach anything else (no default route, DNS fails).

Rootless Podman uses `pasta` or `slirp4netns` for networking; with `--network=none` there is no interface at all, which is what you want. The proxy must run **inside** the container's netns, reached by the agent through `/proc/<pid>/ns/net`.

**Done when:** a documented, repeatable shell script in `testdata/netns-spike.sh` demonstrates all five steps.

**Verify:** `bash testdata/netns-spike.sh` prints PASS for each step

### Task 2.2 — Noise session

`internal/tunnel/noise`. Noise_IK handshake, static keypair per agent and per gateway. Agent knows the gateway's public key from the `TunnelTarget` in the dispatch. Encrypt and decrypt length-prefixed frames.

**Done when:** a round-trip test encrypts 10MB through the session and gets identical bytes back, and a tampered ciphertext fails authentication.

**Verify:** `go test -race ./internal/tunnel/noise/...`

### Task 2.3 — QUIC transport and relay

`internal/tunnel/quic` wraps `quic-go`. Agent and gateway each dial the coordinator's relay endpoint and authenticate with their token.

Coordinator relay: maintains a map of connected gateways. When an agent opens a QUIC stream tagged with a gateway ID and task ID, the relay opens a corresponding stream to that gateway and pumps bytes in both directions, counting them. **The relay never attempts to decrypt; it has no Noise keys.**

One QUIC stream per container TCP connection. Add a 30s idle timeout per stream.

**Done when:** an agent opens a stream, a gateway receives it, bytes flow both ways, and the relay's byte counter matches what both ends sent.

**Verify:** `go test -tags=integration -run TestRelayRoundTrip ./internal/tunnel/...`

### Task 2.4 — Gateway

`cmd/gateway`. Registers with the coordinator using a token, publishes its Noise public key and its service list (`[{name: "db", port: 5432}]`). Holds a QUIC connection to the relay. On an inbound stream: terminate Noise, read the target service name, dial `127.0.0.1:<port>`, pump bytes, count them.

A gateway only forwards to services in its own published list. Anything else is refused.

**Done when:** `gateway --service db:5432` forwards a connection to a local Postgres and reports byte counts.

**Verify:** `go test -tags=integration -run TestGatewayForward ./internal/gateway/...`

### Task 2.5 — In-netns proxy and stub resolver

`internal/agent/netns`. For each task:

1. Enter the container's netns.
2. Bind a stub DNS resolver on `127.0.0.53:53` that answers **only** the hostnames in the task's targets, each with a distinct address from `127.0.10.0/24`.
3. Bind a TCP listener per target on its assigned address and port.
4. Each accepted connection opens a Noise-wrapped QUIC stream to that target's gateway.
5. Write `/etc/resolv.conf` in the container pointing at the stub resolver.

Anything not in the resolver fails to resolve. Anything dialled by raw IP has no route.

**Done when:** a container running `psql -h db.acme.com` reaches the gateway's Postgres, and `curl https://google.com` fails with DNS resolution failure.

**Verify:** `go test -tags=integration -run TestTunnelIsolation ./internal/agent/netns/...`

### Task 2.6 — Egress cap

Count bytes per task in the in-netns proxy. On exceeding `egress_mb`, close all streams for that task, kill the container, and report exit reason `egress_exceeded`.

**Done when:** a task with a 10MB cap transferring 50MB is killed at approximately 10MB.

**Verify:** `go test -tags=integration -run TestEgressCap ./internal/agent/...`

### Task 2.7 — Gateway registration and the three-target rule

`lcctl gateway create --label prod-db` issues a token and prints the install command. `lcctl gateway list`. Submission rejects any task naming more than three gateways, or a gateway not belonging to the submitting account.

**Done when:** submitting with four gateways is rejected with a clear error, and submitting with another account's gateway is rejected.

**Verify:** `go test ./internal/coordinator/api/...`

### Task 2.8 — Phase 2 demo

Extend compose: Postgres (customer's, behind a gateway), the gateway, the coordinator, three agents.

**Done when:** a task runs a container that connects to the customer's Postgres through the tunnel, runs a query, and prints results — while a second task attempting to reach the public internet fails.

**Verify:**
```bash
docker compose -f deploy/docker-compose.tunnel.yml up -d
lcctl submit --image <pg-client-digest> --gateway gw_prod:5432 -- psql -h db.acme.com -c 'select 1'
lcctl submit --image alpine@sha256:<digest> -- wget -T5 https://example.com   # must fail
```

**Record this. "The container can reach the customer's database and nothing else" is the strongest demo in the project.**

---

## 10. Phase 3 — Leases, fencing and orphans

**Goal: no task ever runs twice, and no container ever outlives the agent.**

### Task 3.1 — Lease timing

Coordinator sets `lease_expires_at = now() + lease_s` and `requeue_after = lease_expires_at + margin` (margin 15s). Agent receives `lease_expires_unix_ms` in the dispatch and converts it to a **monotonic** local deadline on arrival.

Agent extends the deadline on each acknowledged heartbeat. Coordinator extends `lease_expires_at` on each heartbeat it receives.

**The invariant to test:** for any sequence of network events, the agent's local fence fires before the coordinator's requeue. It holds because the agent measures from send time and the coordinator from receive time, and receive is never earlier than send.

**Done when:** a property test over random network delays and partitions never produces an interval where the coordinator has requeued and the agent has not yet fenced.

**Verify:** `go test -race -run TestLeaseInvariant ./internal/coordinator/scheduler/...`

### Task 3.2 — Self-fencing

Agent runs a per-task timer. On expiry without a successful heartbeat: `SIGTERM`, wait 10s, `SIGKILL`, mark the task `fenced` locally, and report it on reconnect.

**Done when:** blocking the agent's connection to the coordinator with iptables causes all its containers to die within `lease_s + 10s`.

**Verify:** `go test -tags=integration -run TestSelfFence ./internal/agent/lease/...`

### Task 3.3 — Task adoption on reconnect

On reconnect the agent re-announces running task IDs in `Register`. The coordinator adopts any it still considers assigned to that node and whose requeue has not fired. Any task the coordinator has already requeued is sent a `Cancel`, and the agent kills it.

**Done when:** a 60s network blip with a 120s lease results in the task completing normally with no duplicate dispatch.

**Verify:** `go test -tags=integration -run TestBlipAdoption ./internal/coordinator/...`

### Task 3.4 — Requeue and at-most-once

A reclaimer loop moves tasks past `requeue_after` out of their current state. If `delivery = at_most_once` (the default), the task goes to `abandoned`, not back to the queue. Only `at_least_once` tasks with attempts remaining return to `queued` with `attempt` incremented.

**Done when:** killing an agent mid-task leaves an at-most-once task in `abandoned` and never re-dispatches it.

**Verify:** `go test -tags=integration -run TestAtMostOnce ./internal/coordinator/...`

### Task 3.5 — systemd slice and orphan killing

Write `deploy/systemd/lazycake-agent.service` as a user unit with `Delegate=yes`. The agent launches each container as a transient scope inside its own slice with `KillMode=control-group`.

**Done when:** `kill -9` on the agent process kills every running container within 5 seconds.

**Verify:**
```bash
systemctl --user start lazycake-agent
lcctl submit ... --timeout 300 &
sleep 10
kill -9 $(pgrep -f 'bin/agent')
sleep 5
podman ps --filter label=lazycake.task_id   # must be empty
```

### Task 3.6 — In-container deadline wrapper

A small static Go binary `lcinit`, bind-mounted read-only at `/.lazycake/init`. The container's entrypoint becomes `/.lazycake/init -- <original entrypoint>`. It execs the original as a child, enforces `max_duration` with a hard exit, and forwards signals.

**Done when:** a container whose agent and systemd are both gone still exits at its wall timeout.

**Verify:** `go test -tags=integration -run TestInitDeadline ./cmd/lcinit/...`

### Task 3.7 — Startup reconciliation sweep

On start, the agent lists containers labelled `lazycake.instance_id` and kills every one whose instance ID differs from the current run or whose boot ID differs from `/proc/sys/kernel/random/boot_id`.

**Done when:** after a hard reboot with containers left running, starting the agent kills them before accepting any new work.

**Verify:** `go test -tags=integration -run TestStartupSweep ./internal/agent/...`

---

## 11. Phase 4 — Metering and ledger

**Goal: every task produces a defensible bill and a host credit.**

### Task 4.1 — Benchmark

`internal/agent/bench`. A fixed, deterministic CPU benchmark (a SHA-256 loop plus a small matrix multiply, single-threaded, fixed iteration count). Produces a score relative to a hardcoded reference constant. Runs on registration and every 24 hours, and whenever the coordinator sets `benchmark_now`.

Run it only when the node has no tasks running, or the score is meaningless.

**Done when:** the same machine produces scores within 5% across ten runs.

**Verify:** `go test -run TestBenchStability ./internal/agent/bench/...`

### Task 4.2 — Duration and normalisation

Coordinator computes `duration_s` from its own `TaskStarted` and `TaskFinished` receive times, not from agent-reported timestamps. `normalised_s = duration_s × bench_score`. Write both to `task_meters`.

**Done when:** a node with `bench_score = 0.5` running a task for 20s records `normalised_s = 10`.

**Verify:** `go test ./internal/coordinator/billing/...`

### Task 4.3 — Three-point byte reconciliation

Collect `bytes_agent` from `TaskFinished`, `bytes_relay` from the relay's counters, `bytes_gateway` from the gateway's report. Compute `divergence_pct` as the spread between max and min over the max. Flag anything above 2% and log it with the node ID.

**Done when:** an agent reporting inflated byte counts is flagged while honest agents are not.

**Verify:** `go test -run TestReconciliation ./internal/coordinator/billing/...`

### Task 4.4 — Pricing and ledger

```
price_micros = base_fee
             + (cpu_rate × cores + ram_rate × memory_gb + disk_rate × disk_gb) × normalised_s
             + net_rate × bytes_gateway / 1e9
             + cold_start_fee_if_pulled
```

Rates in a config struct. Every completed task writes a `charge` row against the customer and a `credit` row against the host account, in one transaction that also updates both balances.

Payout rules from `PLAN.md`: non-zero exit is paid; `fenced` pays nothing but is recorded distinctly; `abandoned` pays nothing; cold pull adds a fee to the pulling host.

**Done when:** balances after a run of 100 mixed tasks match a hand-computed expectation, and the ledger sums to the balance for every account.

**Verify:** `go test -tags=integration -run TestLedgerConsistency ./internal/coordinator/billing/...`

### Task 4.5 — Balance enforcement

Reject submission if the account balance cannot cover the worst case (`wall_timeout_s` at full provisioned rate). Deduct a hold at dispatch, settle at completion.

**Done when:** an account with $1 of credit cannot submit a task that could cost $10.

**Verify:** `go test ./internal/coordinator/api/...`

---

## 12. Phase 5 — Trust and canaries

**Goal: catch hosts that lie, without being able to verify results.**

### Task 5.1 — Spec verification

Compare the node's claimed `offer_cores` and reported `bench_score` against measured task durations. A node consistently taking 3x the expected time for its score is either sandbagging or oversubscribed. Track a rolling ratio.

**Done when:** a node artificially throttled to half speed is detected within 20 tasks.

**Verify:** `go test -run TestSpecDrift ./internal/coordinator/scheduler/...`

### Task 5.2 — Canary tasks

The platform owns an account and a gateway. Canary tasks run a known workload against that gateway with a known expected runtime and a known expected output hash reported by the platform's own gateway, not by the agent.

Inject canaries at a rate driven by trust score: 5% at low trust, 0.5% at high trust. Canaries are indistinguishable from real tasks in the dispatch message.

**Done when:** a node returning early without running the workload is caught by the gateway seeing no connection.

**Verify:** `go test -tags=integration -run TestCanaryDetection ./internal/coordinator/...`

### Task 5.3 — Trust score

A score in `[0,1]`, starting at 0.5. Raise on clean completions and passed canaries. Drop sharply on canary failure, byte divergence, spec drift, or abandonment. Below 0.2, stop dispatching and freeze the balance.

Trust affects scheduling priority and canary rate. Document the formula in the README; this is the part reviewers will read.

**Done when:** a simulated dishonest node reaches the ban threshold within 50 tasks while an honest node stays above 0.8.

**Verify:** `go test -run TestTrustConvergence ./internal/coordinator/scheduler/...`

### Task 5.4 — Cache-aware placement

Placement score:

```
score = base_fit
      + cache_bonus × (image_size_gb / 2)
      - queue_penalty × tasks_assigned_to_node
      + trust_weight × trust_score
```

Cap concurrent cold pulls of the same digest at 3 across the fleet; further tasks wait for a warm node or for the pulls to complete.

**Done when:** dispatching 50 tasks with the same 2GB image causes at most 3 concurrent pulls, and later tasks land on warm nodes.

**Verify:** `go test -run TestColdPullLimit ./internal/coordinator/scheduler/...`

---

## 13. Phase 6 — Dashboard and demo

**Goal: someone who has never seen this understands it in thirty seconds.**

### Task 6.1 — SSE event stream

Coordinator publishes events (task state changes, node connect/disconnect, capacity changes) on `GET /events` as server-sent events.

**Done when:** `curl -N localhost:8080/events` streams JSON events as tasks move.

### Task 6.2 — Dashboard

A single self-contained HTML page served by the coordinator. No build step, no npm. Shows: connected nodes with capacity bars and trust scores, a live task table, and a running total of charges and credits.

**Done when:** the page updates live as tasks run, with no refresh.

### Task 6.3 — Reference workload

Pick one with high compute-to-bytes and low memory. **Recommended: a distributed fuzzing harness.** Each task fuzzes a target for N seconds and reports crashes through the tunnel to the customer's gateway. It is genuinely CPU-bound, tolerates churn perfectly, has tiny inputs, and is a real thing people pay for.

Alternative if fuzzing proves fiddly: Monte Carlo option pricing, or a SAT solver over generated instances.

**Done when:** a single `lcctl` command fans out 50 tasks across the fleet and results stream back.

### Task 6.4 — Chaos demo

A script that: submits 50 tasks, waits 10 seconds, `kill -9`s one agent, and shows its in-flight tasks being fenced and (for at-least-once tasks) rescheduled while the dashboard updates live.

**Done when:** the whole sequence runs unattended and the dashboard tells the story without narration.

**Record this as the README's headline GIF.**

### Task 6.5 — README

Structure, in order:

1. One-line description: "LazyCake — a marketplace for leftover CPU."
2. The chaos demo GIF.
3. What it does, in three sentences.
4. Quickstart: `docker compose up`, then one `lcctl submit`.
5. Architecture diagram (the mermaid block from `PLAN.md`).
6. **Design decisions**, with the reasoning: leases over a broker, self-fencing over kill-on-disconnect, the gateway as enforcement boundary, at-most-once by default, normalised billing.
7. **Threat model**, including the honest statement that a host can read the workload and why that is unavoidable without hardware TEEs.
8. **Prior art**: HTCondor, BOINC, Bacalhau, Golem, Flux, and what this does differently.
9. Limitations and what is not built.

Sections 6, 7 and 8 are what a senior engineer reads. Spend real effort there.

---

## 14. Appendix — Exit reason reference

| Reason | Cause | Billed? |
| --- | --- | --- |
| `exited` | Container exited on its own, any code | Yes |
| `oom` | cgroup memory limit hit | Yes |
| `disk` | Disk quota exceeded | Yes |
| `wall_timeout` | Exceeded `wall_timeout_s` | Yes, up to the limit |
| `no_output` | No log output past `no_output_timeout_s` | Yes, up to the limit |
| `egress_exceeded` | Exceeded `egress_mb` | Yes |
| `cancelled` | Customer cancelled | Yes, partial |
| `fenced` | Agent self-fenced on lease expiry | No compute; recorded as honest |
| `abandoned` | Node vanished, lease expired with no word | No |
| `rejected` | Agent admission control refused | No |

## 15. Appendix — Definition of done for the whole project

The project is complete when all of these are true:

- `docker compose up` brings up a working fleet on a clean machine.
- A task runs, reaches the customer's database through the tunnel, and cannot reach anything else.
- Killing an agent with `kill -9` leaves no orphaned containers.
- A network partition longer than the lease fences tasks with no double-execution.
- The ledger sums to every account balance.
- A simulated dishonest node is detected and banned.
- The README carries the chaos GIF, the design decisions section, the threat model, and prior art.
- `make test` passes, `make lint` is clean.
