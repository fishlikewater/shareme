package api

import "testing"

func TestPublishAssignsIncreasingEventSeq(t *testing.T) {
	bus := NewEventBus()
	a := bus.Publish("peer.updated", map[string]string{"id": "a"})
	b := bus.Publish("peer.updated", map[string]string{"id": "b"})

	if a.EventSeq != 1 || b.EventSeq != 2 {
		t.Fatalf("unexpected sequence: %#v %#v", a, b)
	}
}

func TestSinceReturnsEventsAfterSequence(t *testing.T) {
	bus := NewEventBus()
	bus.Publish("peer.updated", map[string]string{"id": "a"})
	second := bus.Publish("peer.updated", map[string]string{"id": "b"})

	events := bus.Since(1)
	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	if events[0].EventSeq != second.EventSeq {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}
