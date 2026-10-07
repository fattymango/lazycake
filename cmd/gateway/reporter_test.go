package main

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/mkassab215/lazycake/internal/gateway/listener"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestReporterSendsDeltasWithTheirContext(t *testing.T) {
	var got []*lazycakev1.ByteReport
	rp := newReporter("gw_1", quiet(), func(r *lazycakev1.ByteReport) error { got = append(got, r); return nil })

	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 10, ToTask: 20})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 1, ToTask: 2, Final: true})

	if len(got) != 2 {
		t.Fatalf("want 2 reports, got %d", len(got))
	}
	if got[0].GatewayId != "gw_1" || got[0].TaskId != "tsk_a" || got[0].Service != "db" || got[0].BytesToLocal != 10 || got[0].BytesToTask != 20 || got[0].Final {
		t.Fatalf("first report wrong: %+v", got[0])
	}
	if !got[1].Final || got[1].BytesToLocal != 1 || got[1].BytesToTask != 2 {
		t.Fatalf("final report wrong: %+v", got[1])
	}
}

// An outage must delay bytes, not lose them: the unsent report is carried into
// the next one for the same task and service, and a close that happened during
// the outage is not forgotten.
func TestReporterCarriesBytesAcrossAnOutage(t *testing.T) {
	failing := true
	var sent []*lazycakev1.ByteReport
	rp := newReporter("gw_1", quiet(), func(r *lazycakev1.ByteReport) error {
		if failing {
			return errors.New("coordinator unreachable")
		}
		sent = append(sent, proto.Clone(r).(*lazycakev1.ByteReport))
		return nil
	})

	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 100, ToTask: 200})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 5, ToTask: 7, Final: true}) // closed during the outage
	failing = false
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 1, ToTask: 1})

	if len(sent) != 1 {
		t.Fatalf("want one delivered report, got %d", len(sent))
	}
	if sent[0].BytesToLocal != 106 || sent[0].BytesToTask != 208 {
		t.Fatalf("carried bytes lost: %+v", sent[0])
	}
	if !sent[0].Final {
		t.Fatal("a connection that closed during the outage must still count as closed")
	}
}

// Different tasks and services never share a carry (no leakage between them).
func TestReporterKeepsCarriesSeparate(t *testing.T) {
	failing := true
	var sent []*lazycakev1.ByteReport
	rp := newReporter("gw_1", quiet(), func(r *lazycakev1.ByteReport) error {
		if failing {
			return errors.New("down")
		}
		sent = append(sent, proto.Clone(r).(*lazycakev1.ByteReport))
		return nil
	})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db", ToLocal: 100})
	rp.Report(listener.ForwardReport{TaskID: "tsk_b", Service: "db", ToLocal: 7})
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "files", ToLocal: 3})
	failing = false
	rp.Report(listener.ForwardReport{TaskID: "tsk_a", Service: "db"})

	if len(sent) != 1 || sent[0].BytesToLocal != 100 {
		t.Fatalf("tsk_a/db should carry exactly its own 100 bytes, got %+v", sent)
	}
}
