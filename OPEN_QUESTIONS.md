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

## Transport security (deferred)

The agent<->coordinator gRPC connection is plaintext (`insecure.NewCredentials()`)
as of task 1.4. Auth is still real (bearer token, hashed, checked against
`api_tokens`), but nothing encrypts the channel yet. TLS on the gRPC
listener is a small, mechanical addition (server cert + `grpc.Creds`) but
adds cert provisioning to the demo's setup story, so it's deferred rather
than built now — tracked here instead of skipped silently. Revisit before
any non-localhost deployment.

