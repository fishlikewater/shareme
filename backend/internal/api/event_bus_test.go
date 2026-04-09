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
