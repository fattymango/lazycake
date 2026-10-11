package portalapi

import (
	"testing"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/store"
)

func at(base time.Time, secs int) time.Time { return base.Add(time.Duration(secs) * time.Second) }

// 1 MB moving in 10 seconds is 0.8 Mbps.
const mb = 1_000_000

func TestComputeRateAveragesTheLastCompleteBinsAndIgnoresTheOpenOne(t *testing.T) {
	// now is 7 s into a bin: that bin is still filling and must not count.
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	now := at(base, 7)
	g := store.Gateway{ID: "gw_a"}
	samples := []store.GatewaySamplePoint{
		{GatewayID: "gw_a", At: at(base, -30), BytesToLocal: 3 * mb, BytesToTask: 0},        // 30 s ago: in the window
		{GatewayID: "gw_a", At: at(base, -20), BytesToLocal: 3 * mb, BytesToTask: mb},       // in the window
		{GatewayID: "gw_a", At: at(base, -10), BytesToLocal: 3 * mb, BytesToTask: 2 * mb},   // newest complete bin
		{GatewayID: "gw_a", At: at(base, 0), BytesToLocal: 900 * mb, BytesToTask: 900 * mb}, // the open bin: ignored
		{GatewayID: "gw_a", At: at(base, -40), BytesToLocal: 90 * mb},                       // before the window: in the series, not in "now"
	}
	r := computeRate(samples, []store.Gateway{g}, now)

	// window = 3 bins = 30 s: rx 9 MB, tx 3 MB -> 2.4 Mbps and 0.8 Mbps
	if r.Now.ReceivedFromTasksBps != 2_400_000 || r.Now.SentToTasksBps != 800_000 {
		t.Fatalf("now = %+v, want 2.4 Mbps received and 0.8 Mbps sent", r.Now)
	}
	if r.AsOfMS != base.UnixMilli() {
		t.Fatalf("as of %d, want the end of the newest complete bin %d", r.AsOfMS, base.UnixMilli())
	}
	if len(r.Series) != 30 {
		t.Fatalf("%d bins, want 30 (five minutes)", len(r.Series))
	}
	last := r.Series[len(r.Series)-1]
	if last.AtMS != at(base, -10).UnixMilli() || last.ReceivedFromTasksBps != 2_400_000 || last.SentToTasksBps != 1_600_000 {
		t.Fatalf("newest bin = %+v", last)
	}
	// the busiest bin is the 90 MB one 40 s ago: 72 Mbps received
	if r.Peak.ReceivedFromTasksBps != 72_000_000 {
		t.Fatalf("peak = %+v, want 72 Mbps received", r.Peak)
	}
	if len(r.Gateways) != 1 || r.Gateways[0].ID != "gw_a" || r.Gateways[0].ReceivedFromTasksBps != 2_400_000 {
		t.Fatalf("gateways = %+v", r.Gateways)
	}
}

func TestComputeRateSplitsGatewaysAndKeepsIdleOnesAndTheirLimit(t *testing.T) {
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	fifty := 50
	gws := []store.Gateway{{ID: "gw_busy", MaxMbps: &fifty}, {ID: "gw_idle"}}
	samples := []store.GatewaySamplePoint{
		{GatewayID: "gw_busy", At: at(base, -10), BytesToLocal: 5 * mb},
		{GatewayID: "gw_busy", At: at(base, -10), BytesToLocal: 5 * mb},            // two tasks in the same bin add up
		{GatewayID: "gw_other_account", At: at(base, -10), BytesToLocal: 999 * mb}, // not one of the listed gateways: in the total, never listed
	}
	r := computeRate(samples, gws, at(base, 3))
	if len(r.Gateways) != 2 {
		t.Fatalf("gateways = %+v, want the busy one and the idle one", r.Gateways)
	}
	busy, idle := r.Gateways[0], r.Gateways[1]
	if busy.ReceivedFromTasksBps != 2_666_666 { // 10 MB over 30 s
		t.Fatalf("busy = %+v", busy)
	}
	if busy.MaxMbps == nil || *busy.MaxMbps != 50 {
		t.Fatalf("the limit must be carried: %+v", busy.MaxMbps)
	}
	if idle.ReceivedFromTasksBps != 0 || idle.SentToTasksBps != 0 || idle.MaxMbps != nil {
		t.Fatalf("an idle gateway with no reported limit must read zero and null: %+v", idle)
	}
}

func TestComputeRateWithNothingIsAllZeroNotMissing(t *testing.T) {
	r := computeRate(nil, nil, time.Now())
	if r.Now.ReceivedFromTasksBps != 0 || r.Peak.SentToTasksBps != 0 || len(r.Series) != 30 {
		t.Fatalf("%+v", r)
	}
	for _, p := range r.Series {
		if p.ReceivedFromTasksBps != 0 || p.SentToTasksBps != 0 {
			t.Fatalf("an empty bin must be a real zero: %+v", p)
		}
	}
}

func TestComputeRateIgnoresBinsOlderThanTheChart(t *testing.T) {
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	r := computeRate([]store.GatewaySamplePoint{{GatewayID: "gw_a", At: at(base, -600), BytesToLocal: 999 * mb}}, []store.Gateway{{ID: "gw_a"}}, at(base, 1))
	if r.Peak.ReceivedFromTasksBps != 0 {
		t.Fatalf("a bin from ten minutes ago leaked into the five-minute series: %+v", r.Peak)
	}
}
