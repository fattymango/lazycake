// Package bench implements IMPLEMENTATION.md task 4.1's fixed CPU
// benchmark: a deterministic, single-threaded SHA-256 loop plus a small
// matrix multiply, producing a score relative to a hardcoded reference
// constant. The coordinator uses this score (task 4.2) to normalise a
// task's wall-clock duration across hosts of different speeds, so it has
// to be stable on a given machine run to run, not just roughly indicative.
package bench

import (
	"crypto/sha256"
	"sort"
	"time"
)

// referenceNanos is the fixed point score = 1.0 was calibrated against:
// one Run() at these exact iteration counts on an arbitrary reference
// machine. It must never change - rescaling it would silently rescale
// every historical bench_score and, with it, every historical
// normalised_s (task 4.2) and price (task 4.4) computed from it.
const referenceNanos = 250_000_000 // 250ms

const (
	shaIterations    = 700_000
	shaBufferBytes   = 4096
	matrixSize       = 220
	matrixMultiplies = 6
)

// sampleTrials is how many timed repetitions Run takes the median of. A
// single wall-clock measurement is vulnerable to one unlucky scheduler
// preemption turning a fast machine's score into a misleadingly slow one;
// the median of a few independent samples throws that single outlier away
// without needing anything fancier.
const sampleTrials = 9

// Run executes the fixed benchmark and returns a score relative to
// referenceNanos: 1.0 matches the reference machine, >1.0 is faster, <1.0
// is slower. It does no I/O and spawns no goroutines, so its own overhead
// is just the algorithm - the caller (IMPLEMENTATION.md task 4.1: "run it
// only when the node has no tasks running") is responsible for making sure
// nothing else is competing for the core while it runs, since that's what
// actually keeps the score meaningful, not anything Run itself can enforce.
func Run() float64 {
	samples := make([]time.Duration, sampleTrials)
	for i := range samples {
		start := time.Now()
		shaLoop()
		matrixLoop()
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	median := samples[len(samples)/2]
	if median <= 0 {
		median = time.Nanosecond
	}
	return float64(referenceNanos) / float64(median.Nanoseconds())
}

// sinks exist purely so the compiler can never conclude these loops'
// results are unused and eliminate them.
var shaSink byte
var matrixSink float64

func shaLoop() {
	buf := make([]byte, shaBufferBytes)
	for i := range buf {
		buf[i] = byte(i)
	}
	sum := sha256.Sum256(buf)
	for i := 0; i < shaIterations; i++ {
		sum = sha256.Sum256(sum[:])
	}
	shaSink = sum[0]
}

func matrixLoop() {
	a := newMatrix(matrixSize)
	b := newMatrix(matrixSize)
	for i := 0; i < matrixMultiplies; i++ {
		a = multiply(a, b)
	}
	matrixSink = a[0][0]
}

func newMatrix(n int) [][]float64 {
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
		for j := range m[i] {
			m[i][j] = float64((i*31+j*17)%97) / 97.0
		}
	}
	return m
}

func multiply(a, b [][]float64) [][]float64 {
	n := len(a)
	c := make([][]float64, n)
	for i := range c {
		c[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			var sum float64
			for k := 0; k < n; k++ {
				sum += a[i][k] * b[k][j]
			}
			c[i][j] = sum
		}
	}
	return c
}
