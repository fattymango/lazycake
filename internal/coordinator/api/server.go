// Package api implements the coordinator side of the agent's long-lived
// gRPC stream (AgentService.Connect): authentication, registration,
// heartbeats, capacity/cache reporting, and task lifecycle events. It talks
// to the rest of the coordinator only through the store.Store and
// Registry interfaces, so the scheduler and billing packages can depend on
// api's exported types without api depending on them back.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// TaskEvents receives task lifecycle events off the agent stream. The
// scheduler and billing packages implement this so api never imports them.
type TaskEvents interface {
	OnTaskAccepted(ctx context.Context, nodeID, taskID string) error
	OnTaskRejected(ctx context.Context, nodeID, taskID, reason string) error
	OnTaskStarted(ctx context.Context, nodeID, taskID string, at time.Time) error
	OnTaskFinished(ctx context.Context, nodeID string, ev TaskFinishedEvent) error
}

// TaskFinishedEvent carries everything TaskFinished reports.
type TaskFinishedEvent struct {
	TaskID        string
	ExitCode      int32
	ExitReason    string
	At            time.Time
	BytesSent     int64
	BytesRecv     int64
	ColdPullBytes int64
}

// CapacityEvents receives capacity reports off the agent stream. The
// scheduler implements this to keep its placement cache current; api never
// imports scheduler.
type CapacityEvents interface {
	OnCapacityReport(ctx context.Context, nodeID string, rep CapacityReportEvent) error
}

// CapacityReportEvent carries what a CapacityReport message reports.
type CapacityReportEvent struct {
	FreeCores    float64
	FreeMemoryMB int32
	FreeDiskMB   int32
}

// Server implements lazycakev1.AgentServiceServer.
type Server struct {
	lazycakev1.UnimplementedAgentServiceServer

	Store      store.Store
	Registry   *Registry
	Events     TaskEvents     // may be nil until the scheduler is wired in (phase 1.8)
	Capacity   CapacityEvents // may be nil until the scheduler is wired in (phase 1.8)
	Clock      clock.Clock
	Log        *slog.Logger
	HeartbeatS int32
	LeaseS     int32
	// LeaseMargin is added on top of LeaseS when computing a task's
	// requeue_after, so the coordinator's reclaim always fires after the
	// agent's own fence deadline (PLAN.md "Lease and fencing", task 3.1's
	// invariant). Defaults to 15s if zero.
	LeaseMargin time.Duration
}

func (s *Server) leaseMargin() time.Duration {
	if s.LeaseMargin <= 0 {
		return 15 * time.Second
	}
	return s.LeaseMargin
}

// noopEvents is used when Server.Events is nil so Connect never panics
// before the scheduler is wired in.
type noopEvents struct{}

func (noopEvents) OnTaskAccepted(context.Context, string, string) error            { return nil }
func (noopEvents) OnTaskRejected(context.Context, string, string, string) error    { return nil }
func (noopEvents) OnTaskStarted(context.Context, string, string, time.Time) error  { return nil }
func (noopEvents) OnTaskFinished(context.Context, string, TaskFinishedEvent) error { return nil }

func (s *Server) events() TaskEvents {
	if s.Events == nil {
		return noopEvents{}
	}
	return s.Events
}

type noopCapacity struct{}

func (noopCapacity) OnCapacityReport(context.Context, string, CapacityReportEvent) error { return nil }

func (s *Server) capacity() CapacityEvents {
	if s.Capacity == nil {
		return noopCapacity{}
	}
	return s.Capacity
}

func (s *Server) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}

// Connect implements the bidirectional agent stream. The first message an
// agent sends must be Register; everything after that is processed in a
// loop until the stream ends.
func (s *Server) Connect(stream lazycakev1.AgentService_ConnectServer) error {
	ctx := stream.Context()

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	reg := first.GetRegister()
	if reg == nil {
		return status.Error(codes.InvalidArgument, "first message must be Register")
	}

	nodeID, needsBenchmark, err := s.handleRegister(ctx, reg)
	if err != nil {
		return err
	}
	log := s.Log.With("node_id", nodeID)

	send := make(chan *lazycakev1.CoordinatorMessage, 64)
	s.Registry.Add(nodeID, send)
	if err := s.Store.SetNodeConnected(ctx, nodeID, true); err != nil {
		log.Error("marking node connected", "error", err)
	}
	defer func() {
		s.Registry.Remove(nodeID, send)
		// Use a background context: stream.Context() is already cancelled here.
		if err := s.Store.SetNodeConnected(context.Background(), nodeID, false); err != nil {
			log.Error("marking node disconnected", "error", err)
		}
		log.Info("agent disconnected")
	}()

	if err := stream.Send(&lazycakev1.CoordinatorMessage{
		Body: &lazycakev1.CoordinatorMessage_RegisterAck{
			RegisterAck: &lazycakev1.RegisterAck{
				NodeId:       nodeID,
				HeartbeatS:   s.HeartbeatS,
				LeaseS:       s.LeaseS,
				BenchmarkNow: needsBenchmark,
			},
		},
	}); err != nil {
		return err
	}
	log.Info("agent registered", "hostname", reg.GetHostname(), "arch", reg.GetArch())
	s.adoptOrCancel(ctx, nodeID, reg.GetRunningTaskIds())

	// Pump outbound messages to the stream concurrently with the inbound
	// receive loop below.
	sendErrCh := make(chan error, 1)
	go func() {
		for msg := range send {
			if err := stream.Send(msg); err != nil {
				sendErrCh <- err
				return
			}
		}
	}()

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.handleMessage(ctx, nodeID, msg); err != nil {
			log.Error("handling agent message", "error", err)
		}

		select {
		case err := <-sendErrCh:
			return err
		default:
		}
	}
}

func (s *Server) handleRegister(ctx context.Context, reg *lazycakev1.Register) (nodeID string, needsBenchmark bool, err error) {
	tok, err := s.Store.Authenticate(ctx, auth.Hash(reg.GetToken()))
	if err != nil {
		return "", false, status.Error(codes.Unauthenticated, "invalid token")
	}
	if tok.Kind != store.TokenAgent {
		return "", false, status.Error(codes.PermissionDenied, "token is not an agent token")
	}

	// Reuse the same node row across reconnects of the same agent process
	// (task 3.3): a fresh node_id every time would mean a fresh, empty set
	// of assigned tasks every time, making adoption impossible.
	nodeID = id.New(id.Node)
	needsBenchmark = true // no prior node found = never benchmarked
	if instanceID := reg.GetInstanceId(); instanceID != "" {
		if existing, err := s.Store.GetNodeByInstanceID(ctx, tok.AccountID, instanceID); err == nil {
			nodeID = existing.ID
			needsBenchmark = existing.BenchScore == nil
		} else if err != store.ErrNotFound {
			return "", false, fmt.Errorf("looking up node by instance id: %w", err)
		}
	}
	caps := reg.GetCaps()
	offer := reg.GetOffer()
	n := store.Node{
		ID:         nodeID,
		AccountID:  tok.AccountID,
		InstanceID: reg.GetInstanceId(),
		Hostname:   reg.GetHostname(),
		Arch:       reg.GetArch(),
		CPUFlags:   reg.GetCpuFlags(),
		Capabilities: store.Capabilities{
			MemoryLimit:   caps.GetMemoryLimit(),
			CPUQuota:      caps.GetCpuQuota(),
			PIDsLimit:     caps.GetPidsLimit(),
			DiskLimit:     caps.GetDiskLimit(),
			GVisor:        caps.GetGvisor(),
			SystemdSlice:  caps.GetSystemdSlice(),
			Runtime:       caps.GetRuntime(),
			CgroupVersion: caps.GetCgroupVersion(),
		},
		OfferCores:    offer.GetCores(),
		OfferMemoryMB: int(offer.GetMemoryMb()),
		OfferDiskMB:   int(offer.GetDiskMb()),
	}
	if err := s.Store.UpsertNode(ctx, n); err != nil {
		return "", false, fmt.Errorf("upserting node: %w", err)
	}
	return nodeID, needsBenchmark, nil
}

// adoptOrCancel implements task 3.3's reconnect adoption: for every task ID
// the agent re-announces as still running in Register, either leave it be
// (the coordinator still considers it assigned to this node and hasn't
// requeued it) or tell the agent to kill it (the coordinator has already
// moved on - reassigned elsewhere or reclaimed - so a duplicate dispatch
// must not be allowed to keep running silently on this node too).
func (s *Server) adoptOrCancel(ctx context.Context, nodeID string, runningTaskIDs []string) {
	now := s.now()
	for _, taskID := range runningTaskIDs {
		t, err := s.Store.GetTask(ctx, taskID)
		if err != nil {
			s.Log.Warn("adoption: looking up reported-running task", "node_id", nodeID, "task_id", taskID, "error", err)
			continue
		}

		stillAssigned := t.NodeID != nil && *t.NodeID == nodeID &&
			(t.State == store.TaskDispatched || t.State == store.TaskRunning) &&
			(t.RequeueAfter == nil || now.Before(*t.RequeueAfter))
		if stillAssigned {
			s.Log.Info("adopting task on reconnect", "node_id", nodeID, "task_id", taskID)
			continue
		}

		s.Log.Warn("task no longer assigned to reconnecting node, cancelling", "node_id", nodeID, "task_id", taskID, "state", t.State)
		if err := s.Registry.Send(nodeID, &lazycakev1.CoordinatorMessage{
			Body: &lazycakev1.CoordinatorMessage_Cancel{Cancel: &lazycakev1.Cancel{TaskId: taskID, Reason: "reclaimed"}},
		}); err != nil {
			s.Log.Warn("sending cancel for reclaimed task", "node_id", nodeID, "task_id", taskID, "error", err)
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, nodeID string, msg *lazycakev1.AgentMessage) error {
	switch body := msg.GetBody().(type) {
	case *lazycakev1.AgentMessage_Heartbeat:
		return s.handleHeartbeat(ctx, nodeID, body.Heartbeat)
	case *lazycakev1.AgentMessage_Capacity:
		return s.handleCapacity(ctx, nodeID, body.Capacity)
	case *lazycakev1.AgentMessage_CacheDelta:
		return s.handleCacheDelta(ctx, nodeID, body.CacheDelta)
	case *lazycakev1.AgentMessage_Accepted:
		return s.events().OnTaskAccepted(ctx, nodeID, body.Accepted.GetTaskId())
	case *lazycakev1.AgentMessage_Rejected:
		return s.events().OnTaskRejected(ctx, nodeID, body.Rejected.GetTaskId(), body.Rejected.GetReason())
	case *lazycakev1.AgentMessage_Started:
		at := time.UnixMilli(body.Started.GetAtUnixMs())
		return s.events().OnTaskStarted(ctx, nodeID, body.Started.GetTaskId(), at)
	case *lazycakev1.AgentMessage_Finished:
		f := body.Finished
		return s.events().OnTaskFinished(ctx, nodeID, TaskFinishedEvent{
			TaskID:        f.GetTaskId(),
			ExitCode:      f.GetExitCode(),
			ExitReason:    f.GetExitReason(),
			At:            time.UnixMilli(f.GetAtUnixMs()),
			BytesSent:     f.GetBytesSent(),
			BytesRecv:     f.GetBytesRecv(),
			ColdPullBytes: f.GetColdPullBytes(),
		})
	case *lazycakev1.AgentMessage_Logs:
		return s.handleLogs(ctx, body.Logs)
	case *lazycakev1.AgentMessage_BenchReport:
		if err := s.Store.SetNodeBenchScore(ctx, nodeID, body.BenchReport.GetScore()); err != nil {
			return fmt.Errorf("recording bench score: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown message type %T", body)
	}
}

func (s *Server) handleHeartbeat(ctx context.Context, nodeID string, hb *lazycakev1.Heartbeat) error {
	now := s.now()
	if err := s.Store.RecordHeartbeat(ctx, nodeID, now); err != nil {
		return fmt.Errorf("recording heartbeat: %w", err)
	}
	newRequeueAfter := now.Add(time.Duration(s.LeaseS)*time.Second + s.leaseMargin())
	if err := s.Store.ExtendNodeRequeue(ctx, nodeID, newRequeueAfter); err != nil {
		return fmt.Errorf("extending node requeue: %w", err)
	}
	return s.Registry.Send(nodeID, &lazycakev1.CoordinatorMessage{
		Body: &lazycakev1.CoordinatorMessage_HeartbeatAck{
			HeartbeatAck: &lazycakev1.HeartbeatAck{Seq: hb.GetSeq()},
		},
	})
}

func (s *Server) handleCapacity(ctx context.Context, nodeID string, cap *lazycakev1.CapacityReport) error {
	if offer := cap.GetOffer(); offer != nil {
		if err := s.Store.SetNodeOffer(ctx, nodeID, offer.GetCores(), int(offer.GetMemoryMb()), int(offer.GetDiskMb())); err != nil {
			return fmt.Errorf("setting node offer: %w", err)
		}
	}
	return s.capacity().OnCapacityReport(ctx, nodeID, CapacityReportEvent{
		FreeCores:    cap.GetFreeCores(),
		FreeMemoryMB: cap.GetFreeMemoryMb(),
		FreeDiskMB:   cap.GetFreeDiskMb(),
	})
}

func (s *Server) handleCacheDelta(ctx context.Context, nodeID string, delta *lazycakev1.CacheDelta) error {
	if len(delta.GetPulled()) > 0 {
		imgs := make([]store.CachedImage, len(delta.GetPulled()))
		for i, img := range delta.GetPulled() {
			imgs[i] = store.CachedImage{NodeID: nodeID, Digest: img.GetDigest(), SizeBytes: img.GetSizeBytes(), LastUsed: s.now()}
		}
		if err := s.Store.RecordCachedImages(ctx, nodeID, imgs); err != nil {
			return fmt.Errorf("recording cached images: %w", err)
		}
	}
	if len(delta.GetEvicted()) > 0 {
		if err := s.Store.RemoveCachedImages(ctx, nodeID, delta.GetEvicted()); err != nil {
			return fmt.Errorf("removing cached images: %w", err)
		}
	}
	return nil
}

func (s *Server) handleLogs(ctx context.Context, batch *lazycakev1.LogBatch) error {
	lines := make([]store.LogLine, len(batch.GetLines()))
	for i, l := range batch.GetLines() {
		lines[i] = store.LogLine{
			TaskID: batch.GetTaskId(),
			Seq:    l.GetSeq(),
			Stream: l.GetStream(),
			At:     time.UnixMilli(l.GetAtUnixMs()),
			Line:   l.GetLine(),
		}
	}
	if err := s.Store.AppendLogs(ctx, lines); err != nil {
		return fmt.Errorf("appending logs: %w", err)
	}
	return nil
}
