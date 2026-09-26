// Package reconcile implements the agent's startup reconciliation sweep
// (IMPLEMENTATION.md task 3.7, PLAN.md "Killing orphans"): before accepting
// any new work, kill every container left behind by a previous run of the
// agent - one whose instance_id differs from this run's (a previous
// process, cleanly stopped or crashed) or whose boot_id differs from the
// current kernel's (survived a hard reboot). It depends on runtime.Runtime
// only through its interface, never on a concrete engine.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mkassab215/lazycake/internal/agent/runtime"
)

const (
	instanceIDLabel = "lazycake.instance_id"
	bootIDLabel     = "lazycake.boot_id"
	taskIDLabel     = "lazycake.task_id"
	agentIDLabel    = "lazycake.agent_id"
)

// Sweep lists every container labelled instanceIDLabel and stops+removes
// any that don't belong to (instanceID, bootID) - this run. Errors
// stopping or removing one stale container are logged and do not abort the
// sweep for the rest; a listing failure is returned since it means the
// sweep couldn't even see what's out there.
//
// agentID scopes the whole sweep to containers this same agent identity
// created: a container labelled with a *different* agentID is left alone
// no matter what its instance/boot ID says, since it belongs to a sibling
// agent process, not a previous run of this one. This only matters for a
// deployment where several agents share one container engine (this repo's
// own deploy/docker-compose.yml demo, three agents against one rootless
// podman socket) - a real one-agent-per-host deployment never has a
// sibling to begin with. Without this check, restarting any one agent
// would sweep away every *other* agent's in-flight task containers too,
// since instance_id is fresh per process start and boot_id is the shared
// host kernel's, so neither one actually distinguishes "my previous run"
// from "a sibling's current one" - caught live: a task's own container
// vanished mid-run, killed by a *different* agent's restart sweep.
func Sweep(ctx context.Context, rt runtime.Runtime, agentID, instanceID, bootID string, log *slog.Logger) error {
	containers, err := rt.LabelledContainers(ctx, instanceIDLabel)
	if err != nil {
		return fmt.Errorf("listing lazycake-labelled containers: %w", err)
	}

	for _, c := range containers {
		if c.Labels[agentIDLabel] != agentID {
			continue // belongs to a different agent sharing this engine
		}
		if c.Labels[instanceIDLabel] == instanceID && c.Labels[bootIDLabel] == bootID {
			continue // belongs to this run
		}
		log.Warn("startup sweep: killing stale container from a previous run",
			"container_id", c.ID, "task_id", c.Labels[taskIDLabel],
			"found_instance_id", c.Labels[instanceIDLabel], "found_boot_id", c.Labels[bootIDLabel])
		if err := rt.Stop(ctx, c.ID, 5*time.Second); err != nil {
			log.Warn("startup sweep: stopping stale container", "container_id", c.ID, "error", err)
		}
		if err := rt.Remove(ctx, c.ID); err != nil {
			log.Warn("startup sweep: removing stale container", "container_id", c.ID, "error", err)
		}
	}
	return nil
}
