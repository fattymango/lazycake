# Handover: getting the local demo running on this VM

This file is a one-time operational handover, written by the Claude Code
session that did the implementation work (on the Windows/WSL2 host) for the
Claude Code session now running on this Ubuntu VM. It is not part of the
project's permanent documentation - once the demo is working, this file can
be deleted. Read `PLAN.md`, `IMPLEMENTATION.md`, `PROGRESS.md` and
`OPEN_QUESTIONS.md` for the actual project design/history; this file is
just "what to do right now, on this machine."

## Why this VM exists

The whole project (all of PLAN.md/IMPLEMENTATION.md, phases 0-6) was already
fully implemented, tested, and documented on a Windows machine running
Podman through a WSL2 "podman-machine-default" VM. That environment turned
out to have two real, unfixable-from-inside-it problems that blocked ever
running a live local demo there:

1. **WSL2's kernel does not enforce cgroup v2 memory limits on tmpfs
   writes at all.** Confirmed directly: `dd`-ing 128MB into a
   `--memory=64m` container's `/dev/shm` completed in full, no OOM kill, no
   `dmesg` OOM event, under a real root systemd session. This is a genuine
   kernel-level gap in that specific WSL2 build (`6.18.33.2-microsoft-standard-WSL2`),
   not a config issue.
2. **The Podman WSL2 machine's own virtual networking was broken** after a
   machine restart (no WAN, broken inter-container UDP, broken port
   forwarding to Windows) - unrelated to #1, host-networking-level, not
   fixable from inside the VM.

This VM (Ubuntu under VMware, real hardware virtualization) exists
specifically to get past both. As of this handover, **#1 is confirmed
fixed here** - see "What's already been verified" below. #2 should not
apply at all since VMware doesn't use the same gvisor-tap-vsock networking
stack Podman's WSL2 machine does, but hasn't been fully exercised yet
(nothing's been dispatched to a running agent yet).

## VM details

- Ubuntu, IP `192.168.80.129` (may change - check `hostname -I` if
  anything stops connecting), user `kassab`.
- SSH access via key auth is working (`~/.ssh/id_ed25519_github` copied
  over from the Windows host, GitHub access confirmed with `ssh -T
  git@github.com`).
- Repo cloned at `~/projects/lazycake` from `git@github.com:fattymango/lazycake.git`.
- Go 1.26.0 installed to `/usr/local/go` (the module requires `go 1.26.0`
  exactly per `go.mod` - Ubuntu's `apt` Go package is almost certainly too
  old, don't use it).
- `podman`, `uidmap`, `slirp4netns`, `fuse-overlayfs` installed via apt.

## What's already been verified on this VM

Ran directly, not through the agent's own probe yet at first:

```sh
podman info --format "{{.Host.CgroupManager}} {{.Host.CgroupsVersion}}"   # note: CgroupsVersion, with an "s"
podman run --rm --memory=64m alpine sh -c \
  "cat /sys/fs/cgroup/memory.max; dd if=/dev/zero of=/dev/shm/fill bs=1M count=128; echo EXIT=\$?"
```

Result: `memory.max` correctly read `67108864` (64MB), and the `dd` write
was genuinely cut short at ~62.5MB with `No space left on device`, exit
code 1 - real, working cgroup v2 memory enforcement. This is correct
kernel behaviour for a tmpfs write hitting `memory.max`: it returns
`ENOSPC` to the writing process rather than invoking the OOM killer (that
only happens for anonymous/heap memory pressure, where a page fault can't
just "fail" the way a write syscall can).

**This exposed a real bug in the project's own code**, not another
environment excuse: `internal/agent/probe/checks_podman.go`'s
`CheckMemoryLimit` only recognised success via `OOMKilled == true`, so it
reported `fail` even though enforcement genuinely worked. Fixed already,
committed and pushed (commit `7580a9e`, "fix: probe.CheckMemoryLimit false
negative on real kernels (ENOSPC vs OOM-kill)") - **the VM needs a `git
pull` to pick this up**, see next section.

## Immediate next steps, in order

### 1. Pull the fix and re-run the probe

```sh
cd ~/projects/lazycake
git pull
go build -o bin/agent ./cmd/agent
./bin/agent probe
```

Last known output (from *before* the fix, for comparison):

```
cgroup_version   pass
subuid           pass
memory_limit     fail - --memory=64m did not OOM-kill a 128MB write; ...
cpu_quota        pass
pids_limit       pass
disk_limit       fail - rootless loop mount failed (<nil>): unshare: write failed /proc/self/uid_map: Operation not permitted; ...
systemd_slice    fail - systemctl --user unavailable (exit 1): Failed to connect to bus: No medium found; enable lingering with 'loginctl enable-linger <user>' and log in via a user session
gvisor           skipped - runsc not found on PATH
agent probe: memory and/or cpu enforcement is not working; this host cannot safely bill provisioned resources
```

Expect `memory_limit` to now show `pass`. **`agent probe`'s overall exit
condition (`Report.OK()`) only requires `memory_limit` and `cpu_quota` to
pass** (see `internal/agent/probe/types.go`) - both were already going to
be `pass` once the fix lands, so the agent should be startable now even
before touching `disk_limit`/`systemd_slice` below. Those two degrade the
advertised capability set (no real disk-limit enforcement, no systemd-slice
orphan cleanup on `kill -9`) but don't block startup - see
`Report.Capabilities()`.

### 2. Fix `systemd_slice` (needed for the real chaos-demo `kill -9` behaviour)

Lingering was enabled with `sudo loginctl enable-linger kassab` in the
*same* SSH session that's still open - `systemd --user` and its D-Bus
session only actually start for a *new* login after that. Fix:

```sh
loginctl show-user kassab -p Linger   # confirm it says Linger=yes
exit                                   # close this SSH session entirely
# reconnect fresh (new `ssh kassab@192.168.80.129` or new terminal),
# then re-run ./bin/agent probe from ~/projects/lazycake
```

If `systemctl --user status` still fails after a fresh login, check
`loginctl list-sessions` and `ps aux | grep "systemd --user"` - there
should be a `systemd --user` process owned by `kassab`. Compare against
what the Windows-side session found troubleshooting this same class of
issue on WSL2 (a stale/wrong `XDG_RUNTIME_DIR` and `DBUS_SESSION_BUS_ADDRESS`
were the actual cause there, not lingering itself) if it's still not
working after a fresh login.

### 3. Fix `disk_limit` (optional - degrades to writable-layer monitoring only, doesn't block anything)

Error was `unshare: write failed /proc/self/uid_map: Operation not
permitted`. Check:

```sh
cat /etc/subuid /etc/subgid
```

If `kassab` isn't listed with a range of at least 65536, that's the fix -
`usermod --add-subuids 100000-165535 --add-subgids 100000-165535 kassab`
(exact range doesn't matter much, just needs to exist and not collide).
Low priority - this only affects one advertised capability
(`Capabilities.DiskLimit`), not whether the agent can run at all.

### 4. Bring up the real demo stack

```sh
cd ~/projects/lazycake
loginctl enable-linger kassab   # if not already done
podman compose -f deploy/docker-compose.yml up -d --build
podman ps -a   # confirm coordinator, gateway, agent1/2/3, postgres, customer-db all Up
```

If `podman compose` isn't available, install the static binary the same
way the Windows-side session did on its WSL2 VM:

```sh
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/docker-compose \
  https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-x86_64
chmod +x ~/.local/bin/docker-compose
export PATH="$HOME/.local/bin:$PATH"   # add to ~/.bashrc too
```

Dashboard: `http://192.168.80.129:8080` (should be directly reachable from
the Windows host's browser over VMware's NAT/bridged network - unlike the
WSL2 case, this doesn't depend on gvisor-tap-vsock port forwarding).

Demo customer token is pre-seeded: `demo-customer-token`. Submit a task:

```sh
export LAZYCAKE_COORDINATOR_ADDR=localhost:7443
export LAZYCAKE_TOKEN=demo-customer-token
go run ./cmd/lcctl submit --image docker.io/library/alpine@sha256:<digest> \
  --cpu 0.5 --memory 128 --disk 256 --timeout 60 -- echo hello
```

Watch it actually queue -> dispatch -> run -> succeed on the dashboard -
this is the thing that could never happen on the Windows/WSL2 side.

### 5. Run the real chaos demo (task 6.4's actual point)

```sh
cd ~/projects/lazycake
podman build --target refworkload -t localhost/refworkload:demo -f deploy/Dockerfile .
podman inspect --format '{{.Id}}' localhost/refworkload:demo   # get the digest
REFWORKLOAD_IMAGE=localhost/refworkload@sha256:<digest-from-above> \
  ./deploy/chaos-demo.sh
```

Watch `http://192.168.80.129:8080` while it runs. It submits 50 tasks,
waits 10s, then `kill -9`s `agent2`. Expected, per the script's own
comments and `PROGRESS.md`'s 3.4/6.4 entries: agent2's in-flight tasks stay
shown as dispatched/running against a now-dead node until their lease
lapses (~75s with this stack's defaults), then move to `queued` and get
picked up by a surviving agent - `attempt` incremented, same task ID, no
double-execution. This is the literal, previously-undemonstrable proof of
IMPLEMENTATION.md's "a network partition longer than the lease fences
tasks with no double-execution."

If it works, this is worth a screen recording - it directly fills the gap
`README.md` currently documents ("no GIF - this dev environment can't run
one"). Not required, but would be a nice close-out if you want it.

## Things to watch out for (found the hard way on the Windows/WSL2 side)

- **`migrate` needing network access to `go run
  github.com/pressly/goose/v3/cmd/goose@v3.28.0`** - this VM should have
  normal internet access (unlike the broken WSL2 machine), so this should
  just work. If it doesn't, that's a real problem worth its own
  investigation, not something to route around.
- **Stale `podman ps` state / port conflicts** if compose is brought up,
  torn down, and rebuilt repeatedly - `podman ps -a` and `podman rm -f` are
  your friends; a stray `rootlessport` process holding a port after a
  container's removed can be found with `ss -ltnp` and killed directly.
- **`XDG_RUNTIME_DIR`** - make sure it's unset or correctly pointing at
  `/run/user/$(id -u)` in whatever shell you run podman commands from. A
  wrong value (e.g. inherited from some other context) was a repeated
  source of confusing, unrelated-looking errors on the WSL2 side.

## Project-wide context (for anything beyond this specific demo task)

- All of PLAN.md/IMPLEMENTATION.md is **fully implemented** - phases 0
  through 6, all committed and pushed to `origin/master`
  (`git@github.com:fattymango/lazycake.git`). This VM's job is purely to
  get a live demo running, not to implement anything new.
- `PROGRESS.md` has a dated, detailed entry for every single task,
  including every bug found and fixed along the way and exactly how each
  was verified. `OPEN_QUESTIONS.md` has every environment limitation and
  open judgment call. Read both before assuming something is broken vs.
  already a documented, deliberate scope boundary (e.g. 5.2's missing
  output-hash verification, 5.4's placement-score formula not wired into
  the real dispatch loop - both intentional, not bugs).
- **Binding instruction from the project owner, must be respected in any
  commit made from this VM too: never add a `Co-Authored-By: Claude ...`
  trailer to commits in this repo.** The remote was deliberately reset
  once already specifically because of this.
- Standard destructive-action caution applies as always: don't force-push,
  don't `git reset --hard` without checking `git status` first, etc.
