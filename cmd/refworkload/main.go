// Command refworkload is IMPLEMENTATION.md task 6.3's reference workload:
// Monte Carlo European option pricing. It fits the task's own criteria -
// genuinely CPU-bound (millions of simulated price paths), tiny inputs
// (six numbers), tolerates churn perfectly (every path is independent, so
// a fenced/killed task just loses that run, nothing to reconcile), and
// low memory (paths are accumulated, never all held at once).
//
// Parameters come from environment variables (LAZYCAKE_MC_*), since a
// task's entrypoint/args are set once at submission and this is meant to
// be dispatched identically across a fleet of nodes with only the seed
// varying per task - see cmd/lcctl's `submit --count`.
//
// The result is always written to stdout as one JSON line (so it's always
// visible via `lcctl logs`, tunnel or not), and additionally streamed to
// LAZYCAKE_TARGET_HOST:LAZYCAKE_TARGET_PORT over TCP if both are set -
// "reports ... through the tunnel to the customer's gateway" (task 6.3),
// exactly the one exception a --network=none task container has.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"
)

// Params are a European call option's Black-Scholes-Merton inputs, plus
// how many simulated price paths to run.
type Params struct {
	Spot   float64 // S0: current underlying price
	Strike float64 // K: strike price
	Vol    float64 // sigma: annualised volatility
	Rate   float64 // r: risk-free rate
	Years  float64 // T: time to expiry, in years
	Paths  int     // number of simulated paths
	Seed   int64
}

// Result is what gets reported, both to stdout and to the gateway.
type Result struct {
	TaskID    string  `json:"task_id"`
	Price     float64 `json:"price"`
	StdErr    float64 `json:"std_error"`
	Paths     int     `json:"paths"`
	ElapsedMS int64   `json:"elapsed_ms"`
}

func loadParams() Params {
	return Params{
		Spot:   envFloat("LAZYCAKE_MC_SPOT", 100),
		Strike: envFloat("LAZYCAKE_MC_STRIKE", 100),
		Vol:    envFloat("LAZYCAKE_MC_VOL", 0.2),
		Rate:   envFloat("LAZYCAKE_MC_RATE", 0.03),
		Years:  envFloat("LAZYCAKE_MC_YEARS", 1.0),
		Paths:  envInt("LAZYCAKE_MC_PATHS", 5_000_000),
		Seed:   int64(envInt("LAZYCAKE_MC_SEED", int(time.Now().UnixNano()))),
	}
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// price runs the Monte Carlo simulation: for each path, draw a terminal
// underlying price under geometric Brownian motion and discount its
// payoff back to present value. Single-threaded and deterministic given
// the same seed, matching internal/agent/bench's own "fixed, deterministic
// workload" style.
func price(p Params) (mean, stderr float64) {
	rng := rand.New(rand.NewSource(p.Seed))
	drift := (p.Rate - 0.5*p.Vol*p.Vol) * p.Years
	diffusion := p.Vol * math.Sqrt(p.Years)
	discount := math.Exp(-p.Rate * p.Years)

	var sum, sumSq float64
	for i := 0; i < p.Paths; i++ {
		z := rng.NormFloat64()
		st := p.Spot * math.Exp(drift+diffusion*z)
		payoff := math.Max(st-p.Strike, 0)
		sum += payoff
		sumSq += payoff * payoff
	}

	n := float64(p.Paths)
	mean = discount * (sum / n)
	variance := (sumSq/n - (sum/n)*(sum/n)) / n
	if variance < 0 {
		variance = 0
	}
	stderr = discount * math.Sqrt(variance)
	return mean, stderr
}

func main() {
	start := time.Now()
	p := loadParams()
	mean, stderr := price(p)

	res := Result{
		TaskID:    os.Getenv("LAZYCAKE_TASK_ID"),
		Price:     mean,
		StdErr:    stderr,
		Paths:     p.Paths,
		ElapsedMS: time.Since(start).Milliseconds(),
	}

	line, err := json.Marshal(res)
	if err != nil {
		fmt.Fprintln(os.Stderr, "refworkload: marshalling result:", err)
		os.Exit(1)
	}
	fmt.Println(string(line))

	host, port := os.Getenv("LAZYCAKE_TARGET_HOST"), os.Getenv("LAZYCAKE_TARGET_PORT")
	if host == "" || port == "" {
		return // no tunnel target configured - stdout is the only report
	}
	if err := report(host, port, line); err != nil {
		// A failed report is worth knowing about but not worth failing
		// the task over - the price was still computed and is already in
		// the logs.
		fmt.Fprintln(os.Stderr, "refworkload: reporting to gateway:", err)
	}
}

func report(host, port string, line []byte) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	if err != nil {
		return fmt.Errorf("dialing %s:%s: %w", host, port, err)
	}
	defer conn.Close()
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing result: %w", err)
	}
	return nil
}
