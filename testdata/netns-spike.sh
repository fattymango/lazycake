#!/bin/bash
# Task 2.1 spike: prove the mechanism PLAN.md's tunnel design depends on -
# a --network=none container has no interface of its own, but an
# unprivileged agent process (the SAME uid that created the container, no
# extra privilege) can still enter its network *and* user namespace
# together, bring up loopback in there, and let something inside the
# container reach a listener bound on that loopback. And prove the
# container genuinely cannot reach anything else: no default route, DNS
# resolution fails outright.
#
# Every step below must print PASS or the script exits non-zero (set -e
# plus explicit checks), so a silent regression here is loud, not quiet -
# this is the riskiest mechanism in the whole project (PLAN.md phase 2
# intro: "rootless network namespace manipulation has a habit of not
# working as documented").
set -euo pipefail

CONTAINER=lazycake-netns-spike
cleanup() {
  podman rm -f "$CONTAINER" >/dev/null 2>&1 || true
  kill "${LISTENER_PID:-0}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== step 1: start a --network=none container ==="
podman rm -f "$CONTAINER" >/dev/null 2>&1 || true
podman run -d --name "$CONTAINER" --network=none docker.io/library/alpine:latest sleep 300 >/dev/null
PID=$(podman inspect -f '{{.State.Pid}}' "$CONTAINER")
echo "PASS: container running, pid=$PID"

USERNS=/proc/$PID/ns/user
NETNS=/proc/$PID/ns/net

echo "=== step 2: enter that container's user+net namespace from the agent (unprivileged) ==="
# Entering --user together with --net is the load-bearing trick: this
# process's real uid is the uid that created the container's user
# namespace (rootless podman maps it to "root" inside), so no extra
# capability is needed - only entering --net alone, without also joining
# --user, would fail with EPERM.
if ! nsenter --user="$USERNS" --net="$NETNS" --preserve-credentials -- true; then
  echo "FAIL: could not enter user+net namespace unprivileged"
  exit 1
fi
echo "PASS: entered user+net namespace as the container's own uid, no extra privilege"

echo "=== step 3: bring up loopback and bind a listener inside it ==="
nsenter --user="$USERNS" --net="$NETNS" --preserve-credentials -- ip link set lo up
nsenter --user="$USERNS" --net="$NETNS" --preserve-credentials -- \
  python3 -c '
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 9999))
s.listen(1)
conn, _ = s.accept()
data = conn.recv(1024)
conn.sendall(data)
conn.close()
' &
LISTENER_PID=$!
sleep 1
echo "PASS: loopback up, listener bound on 127.0.0.1:9999 inside the netns (host pid $LISTENER_PID)"

echo "=== step 4: a process inside the container connects to that listener ==="
# alpine's busybox ships nc but not python3, so the in-container side uses
# nc while the agent-side listener (step 3, run on the host outside the
# container) uses python3.
RESPONSE=$(podman exec "$CONTAINER" sh -c 'printf "hello-from-container" | nc -w2 127.0.0.1 9999')
wait "$LISTENER_PID" 2>/dev/null || true
if [ "$RESPONSE" != "hello-from-container" ]; then
  echo "FAIL: expected echo 'hello-from-container', got '$RESPONSE'"
  exit 1
fi
echo "PASS: container reached the agent-bound loopback listener and got its echo back"

echo "=== step 5: the container cannot reach anything else ==="
ROUTES=$(podman exec "$CONTAINER" ip route show 2>/dev/null || true)
if [ -n "$ROUTES" ]; then
  echo "FAIL: expected no routes, got: $ROUTES"
  exit 1
fi
echo "PASS: no default route (ip route show is empty)"

if podman exec "$CONTAINER" timeout 3 nslookup example.com >/dev/null 2>&1; then
  echo "FAIL: DNS resolution unexpectedly succeeded"
  exit 1
fi
echo "PASS: DNS resolution fails (no route to a resolver, no interface at all)"

echo
echo "ALL STEPS PASSED"
