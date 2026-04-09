package api

import "sync/atomic"

type Event struct {
	EventSeq int64  `json:"eventSeq"`
	Kind     string `json:"kind"`
	Payload  any    `json:"payload"`
}

type EventBus struct {
	next atomic.Int64
}

func NewEventBus() *EventBus {
	return &EventBus{}
}

func (b *EventBus) Publish(kind string, payload any) Event {
	return Event{
		EventSeq: b.next.Add(1),
		Kind:     kind,
		Payload:  payload,
	}
}
