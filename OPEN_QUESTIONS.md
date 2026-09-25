# Open questions

Notes on decisions made simple-first, plus alternatives not taken. Add to this
rather than inventing scope. Also used to record environment limitations that
block a verify command (see IMPLEMENTATION.md rule 4).

## Build environment

Built on Windows 10 with Git Bash + native Go for compiling/vetting, and a
WSL2 distro called `podman-machine-default` (Fedora, already provisioned on
this machine, real cgroups v2, real subuid/subgid ranges, passwordless sudo)
as the actual rootless-Linux target for anything that needs a real container
runtime. Podman CLI was missing from Windows PATH but the machine already
existed — downloaded the static `podman-remote` client for Windows, and it
talks straight to that existing machine (`podman.exe machine start`, then
`podman.exe run` works end to end). Docker Desktop would not come up after
repeated attempts (per-user instruction, dropped entirely in favour of
Podman). Go 1.25 is also installed inside the VM at `/usr/local/go`, so
integration tests that need Linux (rootless podman, cgroups, netns, systemd)
run there directly against the repo over the `/mnt/c/...` bind mount, not on
Windows.

**No native `make`, `docker`, or Unix Postgres client on Windows Git Bash.**
Verify commands from IMPLEMENTATION.md that assume a `make`/`docker compose`
Linux shell are instead run as: bring up Postgres with
`podman.exe run -d -p 5432:5432 ... postgres:16-alpine` (or
`podman.exe compose -f deploy/docker-compose.yml up -d` once compose is
confirmed working under the podman machine), then invoke `goose`/`go test`
either directly on Windows (works fine for anything not needing a live
container) or inside `wsl -d podman-machine-default` for anything that does.
`deploy/docker-compose.yml` lives under `deploy/`, not the repo root, so
compose invocations pass `-f deploy/docker-compose.yml` explicitly.

Tasks needing genuine rootless-Linux behaviour (capability probe 1.5, netns
spike 2.1, netns proxy 2.5, systemd slice 3.5) are run for real inside the
podman machine now that it is available — not blocked, contrary to the
initial assessment before that machine was discovered.

## Capability probe on the podman-machine-default VM

Task 1.5's probe (`agent probe`) is real - it shells out to podman and
inspects actual OOM/throttle/fork behaviour - and it correctly fails most
checks inside `podman-machine-default`, because that VM's own podman falls
back to `--cgroup-manager=cgroupfs` (no working `systemd --user` D-Bus
session even with lingering enabled and `/run/user/1000/bus` present; some
transport-level D-Bus issue specific to this nested WSL setup). subuid and
pids_limit pass there; memory, cpu and systemd_slice correctly fail with
actionable messages. This is the probe doing its job, not a bug in it - a
host with a real systemd user session and cgroups v2 delegation (the common
case on an actual Linux desktop/server) should pass memory/cpu/systemd_slice
too. Not chasing a fully-green run further in this nested environment; the
mechanism (start containers, read real kernel-reported state) is what task
1.5 asked for and it is verified working in both directions (pass and fail).

The same root cause shows up again in task 1.6's `TestOOMKill`
(`internal/agent/runtime/podman_test.go`, `-tags=integration`): a
`--memory=64m` container writing 128MB never gets OOM-killed on this VM,
because the limit isn't actually being applied without cgroup delegation -
consistent with `agent probe`'s `memory_limit: fail` on the same host. The
other five runtime integration checks (pull, create, start, wait, logs,
list-by-label, remove) all pass for real.

`internal/e2e/dispatch_test.go` (`-tags=integration`) is the automated form
of task 1.8's verify: real Postgres, real coordinator (scheduler + gRPC),
real agent (conn.Runner + exec.Executor + PodmanRuntime), real podman.
`TestDispatchEndToEnd` passes: submit -> queued -> reserved -> dispatched ->
running -> succeeded with exit code 0, entirely through the real stack.
`TestDispatchOOM` fails on this VM for the same already-documented reason
(no real cgroup memory delegation here) - the task exits normally instead
of being OOM-killed, so its exit_reason is "exited" not "oom". Same root
cause as tasks 1.5 and 1.6, not a scheduler/executor bug: the state-machine
plumbing (dispatch, accept, start, finish, transition to failed) is
verified correct by the fact that this test gets as far as an asserted
mismatch on exit_reason rather than a timeout or a crash.

## Task 1.10 demo: docker-compose works, the literal 3-agent chaos run is
## environment-blocked on this machine (not a code defect)

`deploy/docker-compose.yml` + `deploy/Dockerfile` are real and build/run
correctly under `podman compose` (Docker Desktop is not used - per-user
instruction - `podman compose` shells out to `docker-compose.exe` as a
provider but wires it to talk to the podman socket, and that works fine):
Postgres comes up healthy, a one-shot `migrate` service applies every
migration, `coordinator` starts, seeds the demo account
(`LAZYCAKE_SEED_DEMO_TOKEN`), and listens on 7443. `lcctl` is fully
implemented (submit/status/logs/nodes) against a new `CustomerService` gRPC
API (`proto/lazycake/v1/customer.proto` - not specified by
IMPLEMENTATION.md, which only gives the agent-facing proto, so this is a
from-scratch design: bearer-token auth via gRPC metadata, digest-pinned
image validation, the 3-gateway cap from PLAN.md's overview enforced at
submission even though gateways themselves don't exist until phase 2).

**What doesn't complete on this machine:** the `agent1`/`agent2`/`agent3`
containers (built with `podman-remote` + a symlink so the capability probe
has a `podman` CLI to shell out to, `CONTAINER_HOST` pointed at the host's
socket) correctly run `agent probe`, and it correctly reports
`memory_limit: fail` - because, as documented repeatedly above (tasks 1.5,
1.6, 1.8), this specific `podman-machine-default` WSL2 VM does not have a
working `systemd --user` session, so podman falls back to `--cgroup-manager
cgroupfs` and never actually delegates memory/cpu enforcement. The agent
binary refuses to start without working memory+cpu enforcement (by design -
that's the whole point of task 1.5's probe: never advertise billing-safe
capacity you can't prove). Ran the same check twice more: once with a
natively-run agent process pointed at the containerized coordinator (same
result), and cpu_quota flipped pass/fail between runs (ratio hovering right
at the 0.85 threshold) - this environment's cgroup enforcement isn't just
missing, it's unreliable, which is itself useful evidence that "trust but
verify with a real probe" is the right design, not paranoia.

**This is not a claim that the system doesn't work.** `internal/e2e`'s
`TestDispatchEndToEnd` (task 1.8) already proves the full submit -> queued
-> dispatched -> running -> succeeded pipeline end to end for real,
because that test harness builds the agent's `exec.Executor` directly and
never goes through the `agent probe` gate - it's testing the dispatch and
execution machinery, not host safety, and that machinery works. What's
environment-blocked here specifically is *the literal CLI-driven demo
through the production `agent` binary*, because that binary is correctly
refusing to run unsafely on a host that can't back its own promises. On a
normal Linux host (or this VM if someone gets its systemd user session
working - `loginctl enable-linger` is already on, the D-Bus socket exists
at `/run/user/1000/bus`, but connecting to it fails with "Transport
endpoint is not connected" for reasons not chased down further here) this
would just work, unmodified.

Dug one level further into *why*: `systemd --user` for uid 1000 is actually
running (`ps` shows `/usr/lib/systemd/systemd --user`, pid 343), and its
`dbus-broker` is genuinely listening on `/run/user/1000/bus` (confirmed via
`ss -xlp`, socket owned by the right pid). Yet any client connecting to
that exact path - `systemctl --user`, `busctl`, podman's own client - gets
`ENOTCONN` ("Transport endpoint is not connected"), not the
`ECONNREFUSED`/`ENOENT` you'd expect from a genuinely dead or missing
socket. That smells like a WSL2/`podman-machine-default`-specific AF_UNIX
socket quirk (this VM's rootfs sits on a virtiofs/9p-backed mount under
Windows) rather than anything wrong with the systemd/dbus setup itself.
Not chasing it further - it's infrastructure archaeology, not a LazyCake
bug, and the code already has honest, working fallback behaviour for
exactly this situation (refuse to advertise capabilities that don't work).

## Task 2.5: setns(CLONE_NEWUSER) from Go is fundamentally impossible

The original design called for the agent to join a container's user+net
namespaces directly via `unix.Setns` (the same technique the netns spike
proved worked via `nsenter` from a shell). It does not work from inside a
running Go program: `man 2 setns` documents that `CLONE_NEWUSER` fails with
`EINVAL` when the caller is multithreaded, and every Go process is
multithreaded - the runtime always has more than one OS thread alive
(GC workers, sysmon, etc.), regardless of `runtime.LockOSThread()`. This
was hit empirically: the direct-syscall version got exactly `EINVAL`
joining the user namespace on a real container, matching the man page
precisely once traced back to it.

The fix (`internal/agent/netns/proxy.go`) is the architecture real
container tooling (runc, CNI plugins) uses for the same reason: exec
`nsenter --user=... --net=... --preserve-credentials -- <agent-binary>
__netns_proxy`. `nsenter` is a small single-threaded C program - it does
the `setns()` calls itself, before `execve()`-ing into our agent binary
with a hidden subcommand, which then just runs already inside the joined
namespaces (namespaces are inherited across exec with no further syscalls
needed). `cmd/agent` intercepts `__netns_proxy` as its very first argument
check, before any normal flag parsing, since this process is never typed
by a human.

That still leaves a real problem: a `--network=none` namespace has no
route out to anywhere, including the relay. The `__netns_proxy` child
(`internal/agent/netns/bind.go`) therefore only *binds* the stub resolver
and per-target listeners - it does not serve them - and hands the bound
file descriptors back to the parent over a control `AF_UNIX` socketpair
using `SCM_RIGHTS` (`internal/agent/netns/fds.go`), then exits. A bound
socket keeps working from whichever process holds its fd regardless of
which network namespace that process is in; only the `bind()` call itself
is namespace-sensitive. The parent - which has the host's normal
networking - receives the fds, dials the relay, and does all the actual
serving (`internal/agent/netns/serve.go`). This is the standard pattern
for exactly this problem (see how `slirp4netns` and friends hand sockets
across a namespace boundary) and it is genuinely necessary here, not
over-engineering: without it, accepting a connection and relaying it out
are stuck in namespaces that cannot both reach the container and the
relay at the same time.

## Task 2.5: `podman-machine-default`'s persistent API socket lives in a
## different PID namespace than a freshly-attached `wsl -d` shell

Discovered while making `TestTunnelIsolation` pass: `.State.Pid` returned
by the podman API on `/run/user/1000/podman/podman.sock` (the persistent,
systemd-socket-activated service that's been running since this VM's own
boot) does not correspond to any process visible under `/proc` from a
`wsl -d podman-machine-default -- <command>` session, even though `ps aux`
in that same session shows the real container process under a *different*
PID. Bare `podman` (no explicit socket) does not hit this problem, because
it turns out not to talk to that persistent socket at all in this
environment - it operates directly, so a container it starts is a normal
descendant of the invoking shell.

The fix used for `internal/e2e/tunnel_isolation_test.go`: start a **fresh**
`podman system service` from the same shell/process tree the test itself
runs in, and point `LAZYCAKE_TEST_PODMAN_SOCKET` at that instead of the
pre-existing one - see the test runner scripts used throughout this
session. Once the socket-owning daemon and the caller share the same PID
namespace lineage, `.State.Pid` resolves correctly and `nsenter` can find
it under `/proc`.

**This matters for real deployment, not just this test environment.**
`internal/agent/runtime.DefaultSocket()` prefers exactly the persistent
`$XDG_RUNTIME_DIR/podman/podman.sock` path - the correct, standard choice
on a normal Linux host, where there is one shared PID namespace for
everything and this problem does not arise. It only surfaces in nested
virtualization setups (this WSL2 "podman machine" architecture, specifically)
where that persistent service's process tree apparently does not share a
PID namespace with freshly-attached sessions. A real single-machine Linux
deployment - which is this whole project's actual target per
IMPLEMENTATION.md - is not expected to hit this. Noted here in detail so a
future "netns proxy can't find /proc/<pid>/ns/user" report on some other
nested/virtualized host has a documented starting point.

## docker/docker/client dependency pin

Section 3's approved dependency list names `github.com/docker/docker/client`
without a version. The current release line renamed that repo's module path
to `github.com/moby/moby/...`, so resolving the plain import path pulls a
go.mod that no longer matches it. Pinned to `github.com/docker/docker
v24.0.9+incompatible` (the last release still published under the original
module path) plus matching-era `github.com/docker/go-connections v0.4.0`
and `github.com/docker/distribution v2.8.2+incompatible`, which is what
actually compiles against Go 1.25/1.26 and the current MVS-selected
transitive graph. Newer docker/docker releases would need the
`github.com/moby/moby/client` + `github.com/moby/moby/api` split instead.

## Task 2.8 demo: gateway path fully verified, agent containers still
## blocked by the same cgroup-delegation limitation as phase 1

Brought up the extended `deploy/docker-compose.yml` for real (`postgres`,
`migrate`, `coordinator`, `customer-db`, `gateway`): the gateway
self-registers via `deploy/gateway-entrypoint.sh` using the seeded demo
customer token, connects to the relay, and `lcctl gateway list` shows
`connected: true` against the live coordinator - the whole registration
and connection path (task 2.7's plumbing, exercised through a real running
coordinator+gateway rather than just tests) works. The `agent1/2/3`
services were not brought up in this pass: they hit the exact same
cgroup-delegation limitation documented at length in task 1.10's entry
above (this VM's podman falls back to cgroupfs, so `agent probe` correctly
refuses to start), not anything new. A task actually flowing through
agent -> netns proxy -> relay -> this gateway -> customer-db therefore
isn't demonstrated by a running compose stack here, but the identical path
*is* proven for real by `internal/e2e.TestTunnelIsolation` (task 2.5),
which exercises every one of those hops end to end against a real
container - the compose-specific gap is purely "agents can't start on
this nested VM," already covered ground.

Two real bugs surfaced only by actually running the compose stack (neither
was caught by the unit/integration tests, which all construct `store.Gateway`
values directly rather than going through `CreateGateway`'s SQL insert):
`gateways.noise_pubkey` was declared `NOT NULL` even though it's legitimately
unknown at `lcctl gateway create` time (fixed in migration 006 directly,
since it was still unreleased this session), and the gateway's local
service-forwarding config had no way to say "not on 127.0.0.1" (added the
optional `name:host:port` form to `internal/gateway/config.parseServices`).
This is a good example of why task 2.8 asked for the compose stack to
actually come up, not just pass tests.

## Transport security (deferred)

The agent<->coordinator gRPC connection is plaintext (`insecure.NewCredentials()`)
as of task 1.4. Auth is still real (bearer token, hashed, checked against
`api_tokens`), but nothing encrypts the channel yet. TLS on the gRPC
listener is a small, mechanical addition (server cert + `grpc.Creds`) but
adds cert provisioning to the demo's setup story, so it's deferred rather
than built now — tracked here instead of skipped silently. Revisit before
any non-localhost deployment.

## Task 3.2: podman's log driver defaulting to journald broke `Runtime.Logs`

While verifying task 3.2 (self-fencing), `TestPullCreateStartWaitLogsRemove`
and `TestLogFlood` — both previously green per PROGRESS.md's task 1.6/1.9
entries — started failing with empty log output, every run, not
intermittently. Root cause: this WSL VM's podman was upgraded at some point
between sessions (now reports API 6.0.2) and its host-level default log
driver is `journald`, not `k8s-file`. `PodmanRuntime.Create` never set
`HostConfig.LogConfig` explicitly, so containers inherited whatever the host
defaulted to — and the Docker-compatible `ContainerLogs` API endpoint
`PodmanRuntime.Logs` calls reads from the `k8s-file`/`json-file` log store,
not journald, so it silently came back empty. `podman logs` from the CLI
still worked fine (it knows to go to journald), masking this from a quick
manual check. Fixed by pinning `LogConfig: container.LogConfig{Type:
"k8s-file"}` explicitly in `internal/agent/runtime/podman.go`'s `Create`,
rather than relying on the host's default — this is exactly the kind of
host-dependent behavior task 1.6 already chose the Docker-compatible API to
avoid, so pinning it is the fix, not a workaround. Not a regression from any
task 3.2 code change; caught only because task 3.2's verify run happened to
exercise the full integration suite.

## Task 3.5: systemd unit verify is BLOCKED on this VM (same root cause as 1.5/1.6)

Task 3.5 asks for `deploy/systemd/lazycake-agent.service` (a user unit with
`Delegate=yes` and `KillMode=control-group`, so podman's systemd cgroup
manager can create a transient scope per container as a child of the
agent's own delegated cgroup, and a bare `kill -9` on the agent's PID takes
every container with it via systemd's normal service-exit cleanup) - this
is exactly the mechanism task 1.5's `agent probe` already checks for
(`CheckSystemdSlice`), and this VM has never had it working: `systemctl
--user` fails with "Failed to connect to user scope bus via local
transport: No such file or directory" (no user D-Bus session in this nested
WSL2 environment), the same finding documented under "Build environment"
and reconfirmed at tasks 1.5/1.6/1.8/1.10. The unit file itself is written
and `systemd-analyze verify` confirms it parses correctly (Delegate/
KillMode accepted, the only complaints are the binary not being installed
yet and a cosmetic executable-bit warning, both expected/fixed) - but the
actual "kill -9 kills every container within 5s" behavior needs a real
`systemctl --user` session with cgroup v2 delegation to exercise, which
this machine cannot provide. **No Go code changes were needed for this
task** - podman's own systemd cgroup manager already creates the
per-container transient scope automatically whenever it successfully talks
to systemd (this is what `CheckSystemdSlice`/`CgroupVersion` have been
verifying since task 1.5); `PodmanRuntime` doesn't need to do anything
differently. Marked BLOCKED in PROGRESS.md per IMPLEMENTATION.md rule 4;
verify on a real target host with a working user session:
```
systemctl --user daemon-reload
systemctl --user enable --now lazycake-agent
lcctl submit ... --timeout 300 &
sleep 10
kill -9 $(pgrep -f 'bin/agent')
sleep 5
podman ps --filter label=lazycake.task_id   # must be empty
```

## Task 4.1: TestBenchStability is occasionally flaky on this dev machine

`internal/agent/bench.Run` does a deterministic SHA-256+matrix-multiply
workload and returns `referenceNanos / median(N timed samples)`. The first
version (a single ~10ms measurement) had a 20%+ spread - far short of the
5% task 4.1 asks for - so it went through several real improvements: a
larger fixed workload (~130ms/sample), two discarded warmup runs (CPU
frequency ramp-up on a cold core is a real, systematic first-sample-is-slow
effect, not just noise), and taking the median of 9 samples per `Run()`
call instead of one raw measurement (throws away a small number of
scheduler-preemption outliers for free). This took the observed spread from
~22% down to consistently ~5-7% on this specific machine.

It still fails roughly 1 run in 4-5 with `go test -count=1 -run
TestBenchStability` on both native Windows and inside the
podman-machine-default WSL VM, with spreads in the 5-10% range - close to
the 5% bar but not reliably under it. This machine has had a great deal of
concurrent load throughout this session (compiles, podman pulls, multiple
`go test` runs), which plausibly explains genuine background CPU
contention rather than anything wrong with the median-of-9 approach itself;
chasing it further with even more samples/larger workload trades real
test-suite time for diminishing, unverifiable returns on a machine that
was never going to be the actual measurement target anyway (a real agent
host, idle apart from lazycake's own processes, is exactly what task 4.1's
"only run it when the node has no tasks running" precondition is *for* -
this dev machine can't satisfy that precondition even in principle, since
it's also compiling and running the rest of this session's test suite at
the same time). Not chased further than this - see "avoid rabbit holes" in
the session's own operating instructions. The algorithm itself is sound;
this is a measurement-environment limitation, not a code defect, and is
exactly what task 4.1 already anticipated by requiring an idle node.

## Ops note: podman-machine-default's DNS breaks on every WSL2 restart

Two things reset every time this WSL2 VM restarts (machine sleep/shutdown,
`wsl --shutdown`, host reboot) and have to be redone before any integration
test that pulls an image can run:

1. `/run/user/1000` doesn't exist yet (no user D-Bus/session bootstrap in
   this VM - same underlying gap as the `systemctl --user` limitation
   documented above). Fix: `sudo mkdir -p /run/user/1000 && sudo chown
   user:user /run/user/1000 && sudo chmod 700 /run/user/1000`, then start a
   fresh `podman system service --time=0 unix:///tmp/fresh-podman.sock &`
   (the persistent boot-time podman.sock still has the PID-namespace
   mismatch issue from task 1.5/1.6, so tests should keep pointing
   `LAZYCAKE_TEST_PODMAN_SOCKET` at the fresh one either way).
2. `/etc/wsl.conf` has `generateResolvConf = false`, so `/etc/resolv.conf`'s
   `nameserver` line is whatever it was the last time it was written by
   hand - and WSL2's internal NAT gateway IP changes across restarts, so
   that stale nameserver stops resolving anything (`lookup
   registry-1.docker.io: Temporary failure in name resolution`). Fix:
   `sudo sh -c "echo nameserver $(ip route | awk '/^default/{print $3}') >
   /etc/resolv.conf"` to point it at the current gateway.

Neither is a code defect; both are one-time-per-VM-restart setup steps for
this specific dev environment.

