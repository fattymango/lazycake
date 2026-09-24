package conn

import "testing"

func TestBackoffDoublesAndCaps(t *testing.T) {
	b := NewBackoff()
	b.rand = func() float64 { return 0.5 } // no jitter: (0.5*2-1)*0.2 = 0

	want := []int64{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i, w := range want {
		got := b.Next()
		if got.Seconds() != float64(w) {
			t.Fatalf("attempt %d: got %v, want %ds", i, got, w)
		}
	}
}

func TestBackoffResetReturnsToMin(t *testing.T) {
	b := NewBackoff()
	b.rand = func() float64 { return 0.5 }
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got.Seconds() != 1 {
		t.Fatalf("after reset: got %v, want 1s", got)
	}
}

func TestBackoffJitterWithinBounds(t *testing.T) {
	b := NewBackoff()
	b.rand = func() float64 { return 1 } // max jitter: +20%
	d := b.Next()
	if d.Seconds() != 1.2 {
		t.Fatalf("got %v, want 1.2s", d)
	}
}
