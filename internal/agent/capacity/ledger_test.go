package capacity

import (
	"errors"
	"sync"
	"testing"
)

func TestAdmitAndRelease(t *testing.T) {
	l := NewLedger(Resources{Cores: 4, MemoryMB: 4096, DiskMB: 10000})

	if err := l.Admit("tsk_1", Resources{Cores: 2, MemoryMB: 2048, DiskMB: 1000}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	free := l.Free()
	if free.Cores != 2 || free.MemoryMB != 2048 {
		t.Fatalf("unexpected free capacity: %+v", free)
	}

	l.Release("tsk_1")
	free = l.Free()
	if free.Cores != 4 || free.MemoryMB != 4096 {
		t.Fatalf("expected full capacity back after release, got %+v", free)
	}
}

func TestSecondTaskRejectedWhenJointlyExceedsMemory(t *testing.T) {
	// Two tasks that individually fit a 3GB node but jointly exceed it.
	l := NewLedger(Resources{Cores: 4, MemoryMB: 3072, DiskMB: 10000})

	if err := l.Admit("tsk_1", Resources{Cores: 1, MemoryMB: 2048, DiskMB: 500}); err != nil {
		t.Fatalf("first task should be admitted: %v", err)
	}

	err := l.Admit("tsk_2", Resources{Cores: 1, MemoryMB: 2048, DiskMB: 500})
	var fitErr ErrDoesNotFit
	if !errors.As(err, &fitErr) {
		t.Fatalf("expected ErrDoesNotFit, got %v", err)
	}
}

func TestAdmitSameTaskTwiceErrors(t *testing.T) {
	l := NewLedger(Resources{Cores: 4, MemoryMB: 4096, DiskMB: 10000})
	if err := l.Admit("tsk_1", Resources{Cores: 1, MemoryMB: 256, DiskMB: 100}); err != nil {
		t.Fatal(err)
	}
	if err := l.Admit("tsk_1", Resources{Cores: 1, MemoryMB: 256, DiskMB: 100}); err == nil {
		t.Fatal("expected error admitting the same task ID twice")
	}
}

func TestConcurrentAdmitNeverOverAllocates(t *testing.T) {
	l := NewLedger(Resources{Cores: 100, MemoryMB: 1000, DiskMB: 100000})
	const n = 50 // each wants 30MB; only 33 fit in 1000MB
	var wg sync.WaitGroup
	admitted := make([]bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := l.Admit(taskID(i), Resources{Cores: 1, MemoryMB: 30, DiskMB: 100})
			admitted[i] = err == nil
		}(i)
	}
	wg.Wait()

	count := 0
	for _, ok := range admitted {
		if ok {
			count++
		}
	}
	if count > 33 {
		t.Fatalf("admitted %d tasks into 1000MB at 30MB each, want <= 33", count)
	}
	free := l.Free()
	if free.MemoryMB < 0 {
		t.Fatalf("over-allocated: free memory went negative: %+v", free)
	}
}

func taskID(i int) string { return "tsk_" + string(rune('a'+i)) }

func TestSetOfferDrainsNotEvicts(t *testing.T) {
	l := NewLedger(Resources{Cores: 4, MemoryMB: 4096, DiskMB: 10000})
	if err := l.Admit("tsk_1", Resources{Cores: 2, MemoryMB: 3000, DiskMB: 5000}); err != nil {
		t.Fatal(err)
	}

	// Lower the offer below what's already allocated.
	l.SetOffer(Resources{Cores: 4, MemoryMB: 2000, DiskMB: 10000})

	// The existing allocation for tsk_1 is untouched (SetOffer never
	// evicts); a new admission that would fit the old offer is now
	// rejected because free capacity is clamped by the lower offer.
	err := l.Admit("tsk_2", Resources{Cores: 1, MemoryMB: 500, DiskMB: 100})
	if err == nil {
		t.Fatal("expected new admission to be rejected after offer was lowered below allocated")
	}
}

func TestClampOffer(t *testing.T) {
	physical := Resources{Cores: 8, MemoryMB: 16384, DiskMB: 500000}

	got := ClampOffer(physical, Resources{Cores: 8, MemoryMB: 16384, DiskMB: 500000})
	if got.Cores != 6 { // 75% of 8
		t.Fatalf("expected cores clamped to 6, got %v", got.Cores)
	}
	if got.MemoryMB != 16384-2048 {
		t.Fatalf("expected memory headroom reserved, got %v", got.MemoryMB)
	}
	if got.DiskMB != 500000-10240 {
		t.Fatalf("expected disk headroom reserved, got %v", got.DiskMB)
	}

	// A conservative request under the cap is left alone.
	got = ClampOffer(physical, Resources{Cores: 2, MemoryMB: 4096, DiskMB: 10000})
	if got.Cores != 2 || got.MemoryMB != 4096 || got.DiskMB != 10000 {
		t.Fatalf("expected conservative request unchanged, got %+v", got)
	}
}
