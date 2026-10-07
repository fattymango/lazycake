package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mkassab215/lazycake/internal/gateway/listener"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type sink struct {
	mu   sync.Mutex
	got  []*lazycakev1.ByteReport
	fail bool
}

func (s *sink) send(r *lazycakev1.ByteReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("coordinator unreachable")
	}
	s.got = append(s.got, proto.Clone(r).(*lazycakev1.ByteReport))
	return nil
}

func (s *sink) reports() []*lazycakev1.ByteReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*lazycakev1.ByteReport(nil), s.got...)
}

// The regression found by the production load test: a task opening hundreds of tiny connections used to cost
// the coordinator one database transaction per connection. Now it is one report per task and service.
func TestAThousandConnectionsBecomeOneReportWithExactTotals(t *testing.T) {
	var s sink
	rp := newReporter("gw_1", quiet(), s.send)

	var wantLocal, wantTask int64
	for i := 0; i < 1000; i++ {
		rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "files", ToLocal: 91, ToTask: 205 + int64(i%7), Final: true})
		wantLocal += 91
		wantTask += 205 + int64(i%7)
	}
	if n := len(s.reports()); n != 0 {
		t.Fatalf("Report must not do I/O: %d reports were sent before a flush", n)
	}
	rp.Flush()

	got := s.reports()
	if len(got) != 1 {
		t.Fatalf("1000 connections must become 1 report, got %d", len(got))
	}
	r := got[0]
	if r.GatewayId != "gw_1" || r.TaskId != "tsk_a" || r.Service != "files" {
		t.Fatalf("report context wrong: %+v", r)
	}
	if r.BytesToLocal != wantLocal || r.BytesToTask != wantTask {
		t.Fatalf("bytes must add up exactly: got %d/%d, want %d/%d", r.BytesToLocal, r.BytesToTask, wantLocal, wantTask)
	}
	if r.Connections != 1000 || !r.Final {
		t.Fatalf("the report must say 1000 connections closed: %+v", r)
	}
}

func TestEachTaskAndServiceGetsItsOwnReportAndNothingLeaksBetweenThem(t *testing.T) {
	var s sink
	rp := newReporter("gw_1", quiet(), s.send)
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 10})
	rp.Report(listener.ForwardReport{TaskID: "tsk_b", Service: "db", ToLocal: 7})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "files", ToLocal: 3})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 5, Final: true})
	rp.Flush()

	by := map[string]*lazycakev1.ByteReport{}
	for _, r := range s.reports() {
		by[r.TaskId+"/"+r.Service] = r
	}
	if len(by) != 3 {
		t.Fatalf("want 3 reports, got %d", len(by))
	}
	if by["tsk_a/db"].BytesToLocal != 15 || by["tsk_a/db"].Connections != 1 || by["tsk_b/db"].BytesToLocal != 7 || by["tsk_a/files"].BytesToLocal != 3 {
		t.Fatalf("leakage or lost bytes: %+v %+v %+v", by["tsk_a/db"], by["tsk_b/db"], by["tsk_a/files"])
	}
	if by["tsk_b/db"].Final || by["tsk_b/db"].Connections != 0 {
		t.Fatal("a task whose connection hasn't closed must not report a close")
	}
}

// An outage must delay bytes, not lose them, and a close during the outage is still counted.
func TestAnOutageDelaysBytesButNeverLosesThemOrTheirCloses(t *testing.T) {
	s := &sink{fail: true}
	rp := newReporter("gw_1", quiet(), s.send)
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 100, ToTask: 200})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 5, ToTask: 7, Final: true})
	rp.Flush() // fails: everything stays pending
	if len(s.reports()) != 0 {
		t.Fatal("nothing can have been delivered during the outage")
	}

	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 1, ToTask: 1, Final: true})
	s.mu.Lock()
	s.fail = false
	s.mu.Unlock()
	rp.Flush()

	got := s.reports()
	if len(got) != 1 {
		t.Fatalf("want one delivered report, got %d", len(got))
	}
	if got[0].BytesToLocal != 106 || got[0].BytesToTask != 208 || got[0].Connections != 2 {
		t.Fatalf("carried bytes or closes lost: %+v", got[0])
	}
	rp.Flush()
	if len(s.reports()) != 1 {
		t.Fatal("a delivered batch must not be sent again")
	}
}

func TestShuttingDownFlushesTheLastInterval(t *testing.T) {
	var s sink
	rp := newReporter("gw_1", quiet(), s.send)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { rp.Run(ctx); close(done) }()

	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 42, Final: true})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	got := s.reports()
	if len(got) != 1 || got[0].BytesToLocal != 42 {
		t.Fatalf("the last interval must be flushed on shutdown, got %+v", got)
	}
}

func TestAnEmptyBatchSendsNothing(t *testing.T) {
	var s sink
	rp := newReporter("gw_1", quiet(), s.send)
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db"})
	rp.Flush()
	if len(s.reports()) != 0 {
		t.Fatal("a batch with no bytes and no closes must not cost an RPC")
	}
}
