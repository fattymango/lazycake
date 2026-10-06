package portalapi

import (
	"context"
	"net/http"
	"time"

	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

// GatewayProber checks a gateway's connectivity. *quic.Relay satisfies it; the
// portal depends on the interface so it can be tested without a real relay.
type GatewayProber interface {
	ProbeGateway(ctx context.Context, gatewayID string, services []string) quic.GatewayProbe
}

// gatewayTestTimeout bounds the whole test, so a gateway that has gone quiet
// can never hold a request open. (Deliberately the only limit: the "Test
// connection" button is throttled in the frontend only; one probe per click
// from an authenticated user is cheap.)
const gatewayTestTimeout = 6 * time.Second

// Result states of a gateway test. The headline answers "is it ready for
// tasks?" and the per-service rows say why not.
const (
	gatewayTestGreen  = "green"  // connected and every published service answered
	gatewayTestYellow = "yellow" // connected, but at least one service didn't answer
	gatewayTestRed    = "red"    // the gateway itself isn't connected
	gatewayTestGrey   = "grey"   // connected, but services couldn't be verified (an older gateway)
)

type gatewayServiceTest struct {
	Name  string `json:"name"`
	Port  int    `json:"port"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Ms    int64  `json:"ms,omitempty"`
	// Answered is false when the gateway never replied about this service.
	Answered bool `json:"answered"`
}

type gatewayTestResponse struct {
	Status     string               `json:"status"`
	Connected  bool                 `json:"connected"`
	RTTMs      int64                `json:"rtt_ms,omitempty"`
	Verified   bool                 `json:"verified"`
	Services   []gatewayServiceTest `json:"services"`
	TestedAtMS int64                `json:"tested_at_ms"`
}

// gatewayTestStatus is the one place the headline is decided.
//
//   - red: the gateway isn't connected, so there is nothing to test. (Not
//     "nothing reachable": a connected gateway whose services all refuse is a
//     different problem with a different fix, and is yellow.)
//   - grey: connected, but the services weren't verified (an older gateway
//     that doesn't know about probes). Never green: green must mean verified.
//   - yellow: connected and verified, but at least one service failed.
//   - green: connected and every service answered.
func gatewayTestStatus(connected, verified bool, services []gatewayServiceTest) string {
	if !connected {
		return gatewayTestRed
	}
	if !verified {
		return gatewayTestGrey
	}
	for _, s := range services {
		if !s.OK {
			return gatewayTestYellow
		}
	}
	return gatewayTestGreen
}

// handleTestGateway is POST .../gateways/{id}/test: ask the relay whether the
// gateway is connected and have it dial each published service locally.
func (s *Server) handleTestGateway(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	gw, err := s.Store.GetGateway(r.Context(), r.PathValue("id"))
	if err != nil || gw.AccountID != sess.AccountID {
		writeError(w, http.StatusNotFound, "gateway not found") // someone else's gateway looks like it doesn't exist
		return
	}
	if s.Prober == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway testing isn't available on this server")
		return
	}

	names := make([]string, len(gw.Services))
	for i, svc := range gw.Services {
		names[i] = svc.Name
	}
	ctx, cancel := context.WithTimeout(r.Context(), gatewayTestTimeout)
	defer cancel()
	probe := s.Prober.ProbeGateway(ctx, gw.ID, names)

	out := gatewayTestResponse{
		Connected: probe.Connected, RTTMs: probe.RTTMs, Verified: probe.Verified,
		Services: make([]gatewayServiceTest, len(gw.Services)), TestedAtMS: time.Now().UnixMilli(),
	}
	for i, svc := range gw.Services {
		t := gatewayServiceTest{Name: svc.Name, Port: svc.Port}
		if i < len(probe.Services) {
			p := probe.Services[i]
			t.OK, t.Error, t.Ms, t.Answered = p.OK, p.Error, p.Ms, p.Answered
		}
		out.Services[i] = t
	}
	out.Status = gatewayTestStatus(out.Connected, out.Verified, out.Services)
	writeJSON(w, http.StatusOK, out)
}
