package portalapi

import (
	"net/http"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// The live bandwidth reading for gateways. It is derived from what the gateways already report (stored in 10-second bins per task and
// gateway), so it is the gateway's own byte count, not an estimate: exact, but averaged over bins and about 10-15 seconds behind real
// time (the gateway reports every few seconds, and the bin still filling is left out because its total is not final).
const (
	rateBin    = 10 * time.Second // the stored resolution
	rateWindow = 30 * time.Second // "now" is the average over the last three complete bins
	rateSeries = 5 * time.Minute  // the chart's history
)

// bpsView is a rate in bits per second, from the customer's side like the traffic totals: "received from tasks" is what the gateway
// handed to the local service, "sent to tasks" what came back.
type bpsView struct {
	ReceivedFromTasksBps int64 `json:"received_from_tasks_bps"`
	SentToTasksBps       int64 `json:"sent_to_tasks_bps"`
}

type ratePointView struct {
	AtMS int64 `json:"at_ms"`
	bpsView
}

type gatewayRateView struct {
	ID string `json:"id"`
	bpsView
	// MaxMbps is the cap the gateway reports it enforces (0 = unlimited); null until it reports one.
	MaxMbps *int `json:"max_mbps"`
}

type gatewayRateResponse struct {
	BinS    int `json:"bin_s"`
	WindowS int `json:"window_s"`
	// AsOfMS is the end of the newest complete bin: the reading is of the time before this.
	AsOfMS int64 `json:"as_of_ms"`
	// Now is the average rate over the last WindowS seconds; Peak the busiest single bin of the series (both directions together).
	Now  bpsView `json:"now"`
	Peak bpsView `json:"peak"`
	// Series is the last five minutes, one entry per complete bin, oldest first; a bin with no traffic is a real zero.
	Series   []ratePointView   `json:"series"`
	Gateways []gatewayRateView `json:"gateways"`
}

func bitsPerSecond(bytes int64, over time.Duration) int64 {
	return int64(float64(bytes) * 8 / over.Seconds())
}

// computeRate turns stored 10-second bins into the live reading as of `now`. gateways are listed in the answer even when idle.
func computeRate(samples []store.GatewaySamplePoint, gateways []store.Gateway, now time.Time) gatewayRateResponse {
	cur := now.Truncate(rateBin) // the bin still filling: left out
	from := cur.Add(-rateSeries)
	winFrom := cur.Add(-rateWindow)

	type pair struct{ rx, tx int64 }
	bins := map[int64]pair{} // all gateways together, by bin start
	perGateway := map[string]pair{}
	var winRX, winTX int64
	for _, p := range samples {
		if !p.At.Before(cur) || p.At.Before(from) {
			continue
		}
		b := bins[p.At.UnixMilli()]
		b.rx += p.BytesToLocal
		b.tx += p.BytesToTask
		bins[p.At.UnixMilli()] = b
		if !p.At.Before(winFrom) {
			winRX += p.BytesToLocal
			winTX += p.BytesToTask
			g := perGateway[p.GatewayID]
			g.rx += p.BytesToLocal
			g.tx += p.BytesToTask
			perGateway[p.GatewayID] = g
		}
	}

	resp := gatewayRateResponse{
		BinS: int(rateBin / time.Second), WindowS: int(rateWindow / time.Second), AsOfMS: cur.UnixMilli(),
		Now:      bpsView{bitsPerSecond(winRX, rateWindow), bitsPerSecond(winTX, rateWindow)},
		Series:   []ratePointView{},
		Gateways: []gatewayRateView{},
	}
	for at := from; at.Before(cur); at = at.Add(rateBin) {
		b := bins[at.UnixMilli()]
		pt := ratePointView{AtMS: at.UnixMilli(), bpsView: bpsView{bitsPerSecond(b.rx, rateBin), bitsPerSecond(b.tx, rateBin)}}
		resp.Series = append(resp.Series, pt)
		if pt.ReceivedFromTasksBps+pt.SentToTasksBps > resp.Peak.ReceivedFromTasksBps+resp.Peak.SentToTasksBps {
			resp.Peak = pt.bpsView
		}
	}
	for _, g := range gateways {
		p := perGateway[g.ID]
		resp.Gateways = append(resp.Gateways, gatewayRateView{ID: g.ID, MaxMbps: g.MaxMbps, bpsView: bpsView{bitsPerSecond(p.rx, rateWindow), bitsPerSecond(p.tx, rateWindow)}})
	}
	return resp
}

// handleGatewayRate is GET .../gateways/rate[?gateway=gw_...]: the live bandwidth of all the account's gateways, or of one.
func (s *Server) handleGatewayRate(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	only := r.URL.Query().Get("gateway")
	gateways, err := s.Store.ListGatewaysByAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading gateways")
		return
	}
	if only != "" {
		var one []store.Gateway
		for _, g := range gateways {
			if g.ID == only {
				one = append(one, g)
			}
		}
		if len(one) == 0 {
			writeError(w, http.StatusNotFound, "gateway not found")
			return
		}
		gateways = one
	}
	now := time.Now()
	samples, err := s.Store.GatewaySamples(r.Context(), sess.AccountID, only, now.Add(-rateSeries-rateBin))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading traffic")
		return
	}
	writeJSON(w, http.StatusOK, computeRate(samples, gateways, now))
}
