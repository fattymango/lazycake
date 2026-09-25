package main

import (
	"bufio"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

// blackScholesCall is the closed-form European call price, used only to
// check price() against ground truth - refworkload itself never computes
// this directly, since the point of the reference workload is to spend
// CPU simulating it, not to look the answer up.
func blackScholesCall(p Params) float64 {
	d1 := (math.Log(p.Spot/p.Strike) + (p.Rate+0.5*p.Vol*p.Vol)*p.Years) / (p.Vol * math.Sqrt(p.Years))
	d2 := d1 - p.Vol*math.Sqrt(p.Years)
	return p.Spot*normCDF(d1) - p.Strike*math.Exp(-p.Rate*p.Years)*normCDF(d2)
}

func normCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

// TestPriceMatchesClosedForm proves the Monte Carlo estimate converges to
// the known Black-Scholes price within a few standard errors - the
// workload has to actually be correct, not just CPU-bound.
func TestPriceMatchesClosedForm(t *testing.T) {
	p := Params{Spot: 100, Strike: 100, Vol: 0.2, Rate: 0.03, Years: 1, Paths: 2_000_000, Seed: 42}
	mean, stderr := price(p)
	want := blackScholesCall(p)

	diff := math.Abs(mean - want)
	// 6 standard errors is an extremely generous bound (should hold well
	// over 99.9999% of the time) while still catching a genuinely broken
	// simulation, not just unlucky sampling.
	if tolerance := 6 * stderr; diff > tolerance {
		t.Fatalf("Monte Carlo price %.4f vs closed-form %.4f: diff %.4f exceeds %.4f (6 stderr)", mean, want, diff, tolerance)
	}
}

func TestPriceIsDeterministicForFixedSeed(t *testing.T) {
	p := Params{Spot: 100, Strike: 100, Vol: 0.2, Rate: 0.03, Years: 1, Paths: 10_000, Seed: 7}
	m1, e1 := price(p)
	m2, e2 := price(p)
	if m1 != m2 || e1 != e2 {
		t.Fatalf("same seed produced different results: (%v,%v) vs (%v,%v)", m1, e1, m2, e2)
	}
}

// TestReportDeliversToRealListener proves the "reports through the tunnel
// to the customer's gateway" half against a real TCP listener, not just
// checking that report() returns no error.
func TestReportDeliversToRealListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer ln.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		received <- line
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("splitting listener address: %v", err)
	}
	if err := report(host, port, []byte(`{"price":1.23}`)); err != nil {
		t.Fatalf("report: %v", err)
	}

	select {
	case line := <-received:
		if !strings.Contains(line, `"price":1.23`) {
			t.Fatalf("received %q, want it to contain the price", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listener never received the report")
	}
}

func TestReportFailsCleanlyOnDeadPort(t *testing.T) {
	// A closed listener on localhost should refuse quickly rather than
	// hanging until some long default timeout - refworkload's own dial
	// has a 10s cap, but this proves the immediate-refusal path works
	// without needing to wait that long.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close() // nothing listening anymore

	if err := report(host, port, []byte("x")); err == nil {
		t.Fatal("expected an error dialing a closed port")
	}
}

func TestPriceIsNonNegative(t *testing.T) {
	// Deep out-of-the-money: price should be tiny but never negative -
	// max(S_T - K, 0) guarantees this algebraically, this just checks the
	// implementation doesn't break that guarantee.
	p := Params{Spot: 50, Strike: 200, Vol: 0.2, Rate: 0.03, Years: 1, Paths: 100_000, Seed: 1}
	mean, _ := price(p)
	if mean < 0 {
		t.Fatalf("price = %v, want >= 0", mean)
	}
}
