package portalapi

import "testing"

// The headline rules, written out as a table because they are the product
// decision: red = not connected, grey = connected but unverified (never green),
// yellow = verified with a failing service, green = all answered.
func TestGatewayTestStatus(t *testing.T) {
	ok := gatewayServiceTest{Name: "a", OK: true, Answered: true}
	bad := gatewayServiceTest{Name: "b", OK: false, Answered: true, Error: "connection refused"}
	silent := gatewayServiceTest{Name: "c", Answered: false}

	cases := []struct {
		name      string
		connected bool
		verified  bool
		services  []gatewayServiceTest
		want      string
	}{
		{"all services answer", true, true, []gatewayServiceTest{ok, ok}, gatewayTestGreen},
		{"one of two fails", true, true, []gatewayServiceTest{ok, bad}, gatewayTestYellow},
		{"every service fails (still connected, so a service-side fix)", true, true, []gatewayServiceTest{bad, bad}, gatewayTestYellow},
		{"not connected", false, false, nil, gatewayTestRed},
		{"not connected, even if a stale result says verified", false, true, []gatewayServiceTest{ok}, gatewayTestRed},
		{"an old gateway that can't verify", true, false, []gatewayServiceTest{silent}, gatewayTestGrey},
		{"an old gateway must never read green, whatever the rows say", true, false, []gatewayServiceTest{ok}, gatewayTestGrey},
		{"no services registered can't be verified", true, false, nil, gatewayTestGrey},
	}
	for _, c := range cases {
		if got := gatewayTestStatus(c.connected, c.verified, c.services); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}
