package desktop

import (
	"context"
	"testing"

	"shareme/backend/internal/api"
)

func TestEventForwarderPublishesToBusAndDesktopEmitter(t *testing.T) {
	bus := api.NewEventBus()

	var (
		gotName  string
		gotEvent api.Event
	)
	forwarder := NewEventForwarder(bus, EventEmitterFunc(func(_ context.Context, eventName string, payload ...any) {
		gotName = eventName
		gotEvent = payload[0].(api.Event)
	}))
	forwarder.SetContext(context.Background())

	forwarder.Publish("peer.updated", map[string]any{"peerDeviceId": "peer-1"})

	if gotName != EventName {
		t.Fatalf("unexpected event name: %s", gotName)
	}
	if gotEvent.EventSeq != 1 || gotEvent.Kind != "peer.updated" {
		t.Fatalf("unexpected emitted event: %#v", gotEvent)
	}
	events := bus.Since(0)
	if len(events) != 1 || events[0].Kind != "peer.updated" {
		t.Fatalf("unexpected bus events: %#v", events)
	}
}

func TestEventForwarderKeepsSequenceWithoutDesktopContext(t *testing.T) {
	bus := api.NewEventBus()
	emitted := 0
	forwarder := NewEventForwarder(bus, EventEmitterFunc(func(context.Context, string, ...any) {
		emitted++
	}))

	forwarder.Publish("message.updated", map[string]any{"messageId": "msg-1"})

	if emitted != 0 {
		t.Fatalf("expected no desktop emit without context, got %d", emitted)
	}
	if bus.LastSeq() != 1 {
		t.Fatalf("unexpected last seq: %d", bus.LastSeq())
	}
}
