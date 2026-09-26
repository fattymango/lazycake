// Package exec turns a coordinator Dispatch message into a running
// container and reports the result back: admission control, pull,
// create/start, wait, and TaskFinished. It depends on runtime.Runtime and
// capacity.Ledger only through their interfaces/exported types, and on the
// coordinator only through the wire messages in lazycakev1 - it never
// imports anything under internal/coordinator.
package exec

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/netns"
	"github.com/mkassab215/lazycake/internal/agent/runtime"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
)

// Sender delivers one AgentMessage to the coordinator. *conn.Runner
// satisfies this structurally.
type Sender interface {
	Send(msg *lazycakev1.AgentMessage)
}

// Executor runs dispatched tasks against a Runtime under a capacity.Ledger.
type Executor struct {
	Runtime    runtime.Runtime
	Ledger     *capacity.Ledger
	Send       Sender
	Log        *slog.Logger
	InstanceID string
	BootID     string
	// AgentID scopes reconcile.Sweep's cleanup to this agent's own
	// containers when several agents share one engine - see sweep.go.
	AgentID string

	// RelayAddr, Token and AgentKeypair are only needed when a dispatched
	// task actually declares tunnel targets; a task with none never
	// touches internal/agent/netns at all.
	RelayAddr    string
	Token        string
	AgentKeypair noise.Keypair

	// LcinitPath, if set, is the host path to the lcinit binary
	// (cmd/lcinit - see IMPLEMENTATION.md task 3.6): every dispatched
	// task's container gets it bind-mounted read-only at
	// /.lazycake/init and its entrypoint rewritten to run through it, so
	// wall_timeout_s is enforced from inside the container even if the
	// agent and systemd are both gone (PLAN.md "Killing orphans"). Empty
	// disables the wrapper entirely - the container runs its own declared
	// entrypoint directly, relying only on the agent-side wall timeout
	// (internal/agent/exec's own runCtx deadline) and task 3.5's
	// systemd-slice cleanup.
	LcinitPath string

	mu      sync.Mutex
	active  map[string]*activeTask              // task_id -> running container
	pending map[string]*lazycakev1.AgentMessage // task_id -> not-yet-confirmed TaskFinished, for replay on reconnect
}

type activeTask struct {
	containerID string
	// overrideReason, once set, replaces whatever exit reason the
	// container's own exit code would otherwise produce - set by FenceAll
	// ("fenced") or CancelTask ("cancelled"), both of which stop the
	// container out from under whatever normal completion path it was on.
	overrideReason string
}

// FenceAll is the agent's self-fencing action (task 3.2, PLAN.md "Lease and
// fencing"): called once internal/agent/lease.Watcher decides the agent
// has lost contact with the coordinator for too long. Every running
// container is stopped (SIGTERM, grace, SIGKILL - see runtime.Stop) and
// marked so its eventual exit is reported as "fenced" rather than
// whatever exit code SIGKILL happens to produce, once there's a
// connection to report it on again (see ReplayPending).
func (e *Executor) FenceAll(ctx context.Context) {
	e.mu.Lock()
	tasks := make(map[string]string, len(e.active))
	for taskID, t := range e.active {
		t.overrideReason = "fenced"
		tasks[taskID] = t.containerID
	}
	e.mu.Unlock()

	for taskID, containerID := range tasks {
		e.Log.Warn("self-fencing task", "task_id", taskID, "container_id", containerID)
		if err := e.Runtime.Stop(ctx, containerID, 10*time.Second); err != nil {
			e.Log.Warn("stopping fenced container", "task_id", taskID, "error", err)
		}
	}
}

// CancelTask stops one task's container in response to a coordinator Cancel
// message - most notably task 3.3's reconnect adoption, where the
// coordinator has already reclaimed or redispatched a task the agent still
// thinks it owns. A no-op if the task isn't currently running (already
// finished, or never was on this agent).
func (e *Executor) CancelTask(ctx context.Context, taskID string) {
	e.mu.Lock()
	t, ok := e.active[taskID]
	if ok {
		t.overrideReason = "cancelled"
	}
	e.mu.Unlock()
	if !ok {
		return
	}
	e.Log.Warn("cancelling task", "task_id", taskID, "container_id", t.containerID)
	if err := e.Runtime.Stop(ctx, t.containerID, 10*time.Second); err != nil {
		e.Log.Warn("stopping cancelled container", "task_id", taskID, "error", err)
	}
}

// ReplayPending resends every TaskFinished the agent couldn't confirm was
// delivered (most notably a fenced task's report, sent while disconnected
// - PLAN.md "report it on reconnect"). Call this from
// conn.Handlers.OnRegistered.
func (e *Executor) ReplayPending() {
	e.mu.Lock()
	pending := e.pending
	e.pending = make(map[string]*lazycakev1.AgentMessage)
	e.mu.Unlock()

	for taskID, msg := range pending {
		e.Log.Info("replaying finished report after reconnect", "task_id", taskID)
		e.Send.Send(msg)
	}
}

func (e *Executor) registerActive(taskID, containerID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active == nil {
		e.active = make(map[string]*activeTask)
	}
	e.active[taskID] = &activeTask{containerID: containerID}
}

// unregisterActive returns the task's override reason, if FenceAll or
// CancelTask set one before this call, so the caller can report the right
// exit reason.
func (e *Executor) unregisterActive(taskID string) (overrideReason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t, ok := e.active[taskID]; ok {
		overrideReason = t.overrideReason
		delete(e.active, taskID)
	}
	return overrideReason
}

// HandleDispatch is a conn.Handlers.OnDispatch-compatible callback: it
// returns immediately and runs the task in its own goroutine so it never
// blocks the connection's receive loop.
func (e *Executor) HandleDispatch(ctx context.Context, d *lazycakev1.Dispatch) {
	go e.run(ctx, d)
}

func (e *Executor) run(ctx context.Context, d *lazycakev1.Dispatch) {
	log := e.Log.With("task_id", d.GetTaskId())

	// A task may finish from several independent places (normal exit,
	// wall timeout, egress cap fired from a background goroutine) - Once
	// keeps the coordinator from seeing more than one TaskFinished for
	// the same task.
	var finishOnce sync.Once
	sendFinished := func(exitCode int32, reason string) {
		finishOnce.Do(func() {
			if override := e.unregisterActive(d.GetTaskId()); override != "" {
				// Fenced/cancelled after this call was already headed for
				// a different reason (e.g. it also hit its wall timeout
				// right as the fence fired) - the override is the more
				// specific, more correct explanation.
				reason = override
				exitCode = -1
			}
			msg := finished(d.GetTaskId(), exitCode, reason, time.Now())
			e.mu.Lock()
			if e.pending == nil {
				e.pending = make(map[string]*lazycakev1.AgentMessage)
			}
			e.pending[d.GetTaskId()] = msg
			e.mu.Unlock()
			e.Send.Send(msg)
		})
	}

	want := capacity.Resources{
		Cores:    d.GetLimits().GetCpuCores(),
		MemoryMB: int(d.GetLimits().GetMemoryMb()),
		DiskMB:   int(d.GetLimits().GetDiskMb()),
	}
	if err := e.Ledger.Admit(d.GetTaskId(), want); err != nil {
		log.Warn("rejecting task, does not fit local capacity", "error", err)
		e.Send.Send(rejected(d.GetTaskId(), err.Error()))
		return
	}
	defer e.Ledger.Release(d.GetTaskId())

	e.Send.Send(accepted(d.GetTaskId()))

	wallTimeout := time.Duration(d.GetLimits().GetWallTimeoutS()) * time.Second
	runCtx := ctx
	var cancel context.CancelFunc
	if wallTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, wallTimeout)
		defer cancel()
	}

	if _, err := e.Runtime.Pull(runCtx, d.GetImage()); err != nil {
		log.Error("pulling image", "image", d.GetImage(), "error", err)
		sendFinished(-1, "error")
		return
	}

	spec, err := e.spec(runCtx, d)
	if err != nil {
		log.Error("resolving container spec", "error", err)
		sendFinished(-1, "error")
		return
	}
	id, err := e.Runtime.Create(runCtx, spec)
	if err != nil {
		log.Error("creating container", "error", err)
		sendFinished(-1, "error")
		return
	}
	// logsDone closes once streamLogs has genuinely finished draining the
	// log stream opened below, having recorded how many lines (if any) it
	// actually relayed. Remove waits on it before firing, and - if
	// streamLogs relayed nothing at all - a fallback non-follow read
	// happens first: a live follow-mode attach can still race a container
	// fast enough to exit before its "watch for new writes" is actually
	// armed, silently missing output that genuinely exists on disk. A
	// non-follow read of an already-exited container has no such race -
	// it just reads what's there - so it's the reliable fallback
	// precisely (and only) when the live path came up empty.
	var linesRelayed int64
	logsDone := make(chan struct{})
	defer func() {
		<-logsDone
		if linesRelayed == 0 {
			e.fallbackLogs(context.Background(), d.GetTaskId(), id)
		}
		if err := e.Runtime.Remove(context.Background(), id); err != nil {
			log.Warn("removing container", "container_id", id, "error", err)
		}
	}()

	if err := e.Runtime.Start(runCtx, id); err != nil {
		close(logsDone)
		log.Error("starting container", "error", err)
		sendFinished(-1, "error")
		return
	}
	e.Send.Send(started(d.GetTaskId(), time.Now()))
	e.registerActive(d.GetTaskId(), id)

	// Tunnel setup runs before attaching logs, not after: Runtime.Logs
	// below can block for a long time against a container that hasn't
	// produced any output yet (podman's REST API doesn't flush a follow-
	// mode logs response's headers until the container writes its first
	// byte), and startTunnel needs to run while the container is still
	// alive. Caught live: every task with tunnel_targets and no early
	// stdout failed with "container state improper", because by the time
	// the (synchronous, blocked-on-Logs) code finally reached startTunnel,
	// the container had already exited. This ordering costs tunnel-less
	// tasks nothing (startTunnel is a no-op below when there are no
	// targets) and only delays log-attach by however long Setup itself
	// takes (milliseconds), which is still well within "right after
	// Start, not before it" - see the comment on the log-attach below for
	// why that ordering (vs. pre-Start) matters.
	if len(d.GetTargets()) > 0 {
		proxy, err := e.startTunnel(ctx, id, d, sendFinished)
		if err != nil {
			// The container is already running with --network=none and no
			// route anywhere; failing to set up its one exception is the
			// same as the task simply being unable to reach its target, so
			// fail the task rather than let it run uselessly to timeout.
			log.Error("starting netns proxy", "error", err)
			close(logsDone)
			_ = e.Runtime.Stop(context.Background(), id, 5*time.Second)
			sendFinished(-1, "error")
			return
		}
		defer proxy.Close()
	}

	// Attached right after Start, not before it: a follow-mode log stream
	// opened against a container that hasn't started yet doesn't reliably
	// pick up writes from the process once it does start (verified live -
	// a 15-minute real workload relayed zero lines the whole run when this
	// was attached pre-Start, only catching up at the very end via
	// fallbackLogs; attaching post-Start, the normal order, streams live
	// exactly as expected). The residual race this reintroduces - a
	// container fast enough to exit before this line even runs - is
	// exactly what fallbackLogs above exists to catch, so nothing is lost
	// either way; it's a narrower window than the old post-goroutine-
	// scheduling version, not a wider one.
	logsRC, logsErr := e.Runtime.Logs(ctx, id, true)
	if logsErr != nil {
		log.Warn("attaching log stream", "container_id", id, "error", logsErr)
		close(logsDone)
	} else {
		go func() {
			defer close(logsDone)
			linesRelayed = e.streamLogs(ctx, d.GetTaskId(), logsRC)
		}()
	}

	result, err := e.Runtime.Wait(runCtx, id)
	if err != nil {
		if runCtx.Err() != nil {
			log.Warn("task exceeded wall timeout, stopping", "wall_timeout_s", d.GetLimits().GetWallTimeoutS())
			_ = e.Runtime.Stop(context.Background(), id, 10*time.Second)
			sendFinished(-1, "wall_timeout")
			return
		}
		log.Error("waiting for container", "error", err)
		sendFinished(-1, "error")
		return
	}

	reason := "exited"
	if result.OOMKilled {
		reason = "oom"
	}
	log.Info("task finished", "exit_code", result.ExitCode, "exit_reason", reason)
	sendFinished(int32(result.ExitCode), reason)
}

// startTunnel sets up internal/agent/netns for a dispatched task that
// declared tunnel targets: a --network=none container otherwise has no
// interface at all, so this is the one exception PLAN.md's design carves
// out, built inside the container's own network namespace.
func (e *Executor) startTunnel(ctx context.Context, containerID string, d *lazycakev1.Dispatch, sendFinished func(int32, string)) (*netns.Proxy, error) {
	pid, err := e.Runtime.Pid(ctx, containerID)
	if err != nil {
		return nil, err
	}

	targets := make([]netns.Target, len(d.GetTargets()))
	for i, t := range d.GetTargets() {
		targets[i] = netns.Target{
			GatewayID: t.GetGatewayId(), Hostname: t.GetHostname(), Port: t.GetPort(),
			NoisePubkey: t.GetNoisePubkey(),
		}
	}

	proxy := &netns.Proxy{
		ContainerPID: pid, ContainerID: containerID, TaskID: d.GetTaskId(),
		Targets: targets, AgentKeypair: e.AgentKeypair,
		RelayAddr: e.RelayAddr, Token: e.Token,
		EgressCapBytes: int64(d.GetLimits().GetEgressMb()) << 20,
		Runtime:        e.Runtime, Log: e.Log,
		OnEgressExceeded: func() {
			e.Log.Warn("egress cap exceeded, stopping task", "task_id", d.GetTaskId())
			_ = e.Runtime.Stop(context.Background(), containerID, 5*time.Second)
			sendFinished(-1, "egress_exceeded")
		},
	}
	if err := proxy.Setup(ctx); err != nil {
		return nil, err
	}
	return proxy, nil
}

func (e *Executor) spec(ctx context.Context, d *lazycakev1.Dispatch) (runtime.Spec, error) {
	entrypoint, args, mounts := d.GetEntrypoint(), d.GetArgs(), []runtime.Mount(nil)
	if e.LcinitPath != "" {
		var err error
		entrypoint, args, mounts, err = e.wrapWithLcinit(ctx, d)
		if err != nil {
			return runtime.Spec{}, err
		}
	}
	return runtime.Spec{
		Name:       fmt.Sprintf("lazycake-%s-%d", d.GetTaskId(), d.GetAttempt()),
		Image:      d.GetImage(),
		Entrypoint: entrypoint,
		Args:       args,
		Env:        d.GetEnv(),
		Workdir:    d.GetWorkdir(),
		Isolation:  d.GetIsolation(),
		CPUCores:   d.GetLimits().GetCpuCores(),
		MemoryMB:   int(d.GetLimits().GetMemoryMb()),
		TmpfsMB:    int(d.GetLimits().GetTmpfsMb()),
		PIDs:       int(d.GetLimits().GetPids()),
		Mounts:     mounts,
		Labels: map[string]string{
			"lazycake.task_id":     d.GetTaskId(),
			"lazycake.instance_id": e.InstanceID,
			"lazycake.boot_id":     e.BootID,
			"lazycake.agent_id":    e.AgentID,
		},
	}, nil
}

// wrapWithLcinit rewrites a task's entrypoint to run through
// /.lazycake/init (task 3.6): lcinit execs the original entrypoint+args as
// its own child, so the container's actual PID 1 is lcinit, still
// enforcing wall_timeout_s even if the agent process and systemd unit that
// dispatched it are both gone by the time it matters.
//
// lcinit has to be told explicitly what to exec - unlike a normal
// container start, the engine never gets a chance to apply the image's own
// Entrypoint/Cmd, since lcinit itself is what's actually configured as the
// container's entrypoint. So when the dispatch didn't override the
// command (the common case for an image like cmd/refworkload that's meant
// to just run via its own ENTRYPOINT), this resolves the image's default
// here instead of silently handing lcinit nothing to run.
func (e *Executor) wrapWithLcinit(ctx context.Context, d *lazycakev1.Dispatch) (entrypoint, args []string, mounts []runtime.Mount, err error) {
	taskEntrypoint, taskArgs := d.GetEntrypoint(), d.GetArgs()
	if len(taskEntrypoint) == 0 && len(taskArgs) == 0 {
		taskEntrypoint, taskArgs, err = e.Runtime.ImageEntrypoint(ctx, d.GetImage())
		if err != nil {
			return nil, nil, nil, fmt.Errorf("resolving default command for %s: %w", d.GetImage(), err)
		}
	}

	lcinitArgs := []string{}
	if wallTimeoutS := d.GetLimits().GetWallTimeoutS(); wallTimeoutS > 0 {
		lcinitArgs = append(lcinitArgs, fmt.Sprintf("--max-duration=%ds", wallTimeoutS))
	}
	if len(d.GetTargets()) > 0 {
		// Closes the race where the task's own command could run before
		// startTunnel (run()) had actually finished wiring up its one
		// network exception - see netns.ReadyFilePath's doc comment.
		lcinitArgs = append(lcinitArgs, "--wait-file="+netns.ReadyFilePath)
	}
	lcinitArgs = append(lcinitArgs, "--")
	lcinitArgs = append(lcinitArgs, taskEntrypoint...)
	lcinitArgs = append(lcinitArgs, taskArgs...)

	return []string{"/.lazycake/init"}, lcinitArgs, []runtime.Mount{
		{HostPath: e.LcinitPath, ContainerPath: "/.lazycake/init", ReadOnly: true},
	}, nil
}

func rejected(taskID, reason string) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Rejected{
		Rejected: &lazycakev1.TaskRejected{TaskId: taskID, Reason: reason},
	}}
}

func accepted(taskID string) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Accepted{
		Accepted: &lazycakev1.TaskAccepted{TaskId: taskID},
	}}
}

func started(taskID string, at time.Time) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Started{
		Started: &lazycakev1.TaskStarted{TaskId: taskID, AtUnixMs: at.UnixMilli()},
	}}
}

func finished(taskID string, exitCode int32, reason string, at time.Time) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Finished{
		Finished: &lazycakev1.TaskFinished{
			TaskId: taskID, ExitCode: exitCode, ExitReason: reason, AtUnixMs: at.UnixMilli(),
		},
	}}
}
