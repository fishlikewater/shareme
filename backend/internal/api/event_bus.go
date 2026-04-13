package api

import (
	"sync"
	"sync/atomic"
)

type Event struct {
	EventSeq int64  `json:"eventSeq"`
	Kind     string `json:"kind"`
	Payload  any    `json:"payload"`
}

type EventBus struct {
	next           atomic.Int64
	nextSubscriber atomic.Int64
	mu             sync.RWMutex
	log            []Event
	subscribers    map[int64]chan Event
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[int64]chan Event),
	}
}

func (b *EventBus) Publish(kind string, payload any) Event {
	event := Event{
		EventSeq: b.next.Add(1),
		Kind:     kind,
		Payload:  payload,
	}

	b.mu.Lock()
	b.log = append(b.log, event)
	subscribers := make([]chan Event, 0, len(b.subscribers))
	for _, subscriber := range b.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	b.mu.Unlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}

	return event
}

func (b *EventBus) Since(afterSeq int64) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()

	events := make([]Event, 0, len(b.log))
	for _, event := range b.log {
		if event.EventSeq > afterSeq {
			events = append(events, event)
		}
	}
	return events
}

func (b *EventBus) LastSeq() int64 {
	return b.next.Load()
}

func (b *EventBus) Subscribe(afterSeq int64) ([]Event, <-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	backlog := make([]Event, 0, len(b.log))
	for _, event := range b.log {
		if event.EventSeq > afterSeq {
			backlog = append(backlog, event)
		}
	}

	subscriberID := b.nextSubscriber.Add(1)
	stream := make(chan Event, 16)
	b.subscribers[subscriberID] = stream

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers, subscriberID)
	}

	return backlog, stream, unsubscribe
}
