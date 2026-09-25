#!/bin/sh
# IMPLEMENTATION.md task 6.4's chaos demo: submits 50 tasks, waits 10s,
# kill -9s one agent, and lets the dashboard show its in-flight work
# getting reclaimed and rescheduled onto a surviving node - live, no
# narration needed once it's running (this script's own stdout is a log
# for whoever's driving it, not something to read off during the
# recording - watch http://localhost:8080 instead).
#
# Run against an already-running `podman compose -f
# deploy/docker-compose.yml up` stack. Needs a digest-pinned
# cmd/refworkload image (task 6.3) already built/available to the agents -
# see REFWORKLOAD_IMAGE below.
#
# What actually happens, precisely (worth being exact about rather than
# reaching for "fenced" loosely): kill -9 gives the agent process no
# chance to run its own shutdown path, so it cannot self-fence (task 3.2)
# - that mechanism is for an agent that's still alive but has lost contact
# with the coordinator, not a dead one. What the dashboard will actually
# show is the coordinator's own reclaimer (task 3.4) noticing the dead
# node's tasks never send another heartbeat, moving each one to `queued`
# once its lease (lease_s + margin, ~75s with this stack's defaults) lapses,
# and a surviving agent picking it up from there - at_least_once, attempt
# incremented, same task ID. This is exactly "the ledger sums to every
# account balance" and "a network partition longer than the lease fences
# tasks with no double-execution" (IMPLEMENTATION.md's definition of done)
# playing out from a real kill -9, not a simulation of it.
set -eu

COMPOSE_FILE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/docker-compose.yml"
COMPOSE="${COMPOSE:-podman compose -f $COMPOSE_FILE}"
LAZYCAKE_COORDINATOR_ADDR="${LAZYCAKE_COORDINATOR_ADDR:-localhost:7443}"
LAZYCAKE_TOKEN="${LAZYCAKE_TOKEN:-demo-customer-token}"
REFWORKLOAD_IMAGE="${REFWORKLOAD_IMAGE:?set REFWORKLOAD_IMAGE to the digest-pinned refworkload image, e.g. localhost/refworkload@sha256:...}"
TASK_COUNT="${TASK_COUNT:-50}"
VICTIM_AGENT="${VICTIM_AGENT:-agent2}"

export LAZYCAKE_COORDINATOR_ADDR LAZYCAKE_TOKEN

echo "==> Dashboard: http://localhost:8080 - open it now, before continuing."
echo "==> Submitting $TASK_COUNT at_least_once tasks (image: $REFWORKLOAD_IMAGE)..."

lcctl submit --count "$TASK_COUNT" --image "$REFWORKLOAD_IMAGE" \
  --cpu 0.5 --memory 128 --disk 256 --timeout 120 --delivery at_least_once \
  --idempotency-key "chaos-$(date +%s)" \
  >/tmp/chaos-demo-task-ids.txt

echo "==> Submitted $(wc -l </tmp/chaos-demo-task-ids.txt) tasks. Waiting 10s for the fleet to pick them up..."
sleep 10

echo "==> kill -9'ing $VICTIM_AGENT..."
$COMPOSE kill -s SIGKILL "$VICTIM_AGENT"

echo "==> Done. $VICTIM_AGENT's tasks show as dispatched/running to a now-dead node until its"
echo "    lease lapses (~75s with this stack's defaults), then move to queued and get picked up"
echo "    by whichever surviving agent claims them next - watch the dashboard, not this terminal."
