package events

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case e, ok := <-ch:
		return e, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}, false
	}
}

func assertNoEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case e, ok := <-ch:
		t.Fatalf("expected no event, got %+v (ok=%v)", e, ok)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSubscribeReceivesEveryEvent(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Publish(Event{Type: "task_state", AccountID: "act_1", TaskID: "tsk_1"})
	b.Publish(Event{Type: "task_state", AccountID: "act_2", TaskID: "tsk_2"})

	e, _ := recv(t, ch)
	if e.TaskID != "tsk_1" {
		t.Fatalf("got %q, want tsk_1", e.TaskID)
	}
	e, _ = recv(t, ch)
	if e.TaskID != "tsk_2" {
		t.Fatalf("got %q, want tsk_2", e.TaskID)
	}
}

func TestSubscribeAccountFiltersToOwnAccount(t *testing.T) {
	b := NewBus()
	ch, cancel := b.SubscribeAccount("act_1")
	defer cancel()

	b.Publish(Event{Type: "task_state", AccountID: "act_2", TaskID: "not_mine"})
	b.Publish(Event{Type: "task_state", AccountID: "act_1", TaskID: "mine"})

	e, _ := recv(t, ch)
	if e.TaskID != "mine" {
		t.Fatalf("got %q, want mine (act_2's event must have been filtered out)", e.TaskID)
	}
	assertNoEvent(t, ch)
}

func TestSubscribeAccountIgnoresEmptyAccountEvents(t *testing.T) {
	// An event published with no AccountID (e.g. a lookup failure at the
	// publish site) must never leak into an account-scoped subscriber -
	// only an exact match, never a wildcard, satisfies a non-empty filter.
	b := NewBus()
	ch, cancel := b.SubscribeAccount("act_1")
	defer cancel()

	b.Publish(Event{Type: "capacity", NodeID: "nod_1"})
	assertNoEvent(t, ch)
}

func TestPublishNeverBlocksOnSlowSubscriber(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	defer cancel()

	// Fill the subscriber's buffer (capacity 32) without ever reading, then
	// publish one more - this must return immediately, not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 40; i++ {
			b.Publish(Event{Type: "task_state", TaskID: "x"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber channel")
	}
	_ = ch
}

func TestNilBusPublishIsNoop(t *testing.T) {
	var b *Bus
	b.Publish(Event{Type: "task_state"}) // must not panic
}

func TestCancelStopsDelivery(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	cancel()

	b.Publish(Event{Type: "task_state", TaskID: "after-cancel"})

	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after cancel")
	}
}
