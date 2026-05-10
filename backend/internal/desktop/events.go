package desktop

import (
	"context"
	"sync"

	"shareme/backend/internal/api"
)

const EventName = "shareme:event"

type EventEmitter interface {
	Emit(ctx context.Context, eventName string, payload ...any)
}

type EventEmitterFunc func(ctx context.Context, eventName string, payload ...any)

func (f EventEmitterFunc) Emit(ctx context.Context, eventName string, payload ...any) {
	f(ctx, eventName, payload...)
}

type EventForwarder struct {
	mu      sync.RWMutex
	ctx     context.Context
	bus     *api.EventBus
	emitter EventEmitter
}

func NewEventForwarder(bus *api.EventBus, emitter EventEmitter) *EventForwarder {
	if bus == nil {
		bus = api.NewEventBus()
	}
	return &EventForwarder{
		bus:     bus,
		emitter: emitter,
	}
}

func (f *EventForwarder) SetContext(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctx = ctx
}

func (f *EventForwarder) Publish(kind string, payload any) {
	event := f.bus.Publish(kind, payload)

	f.mu.RLock()
	ctx := f.ctx
	emitter := f.emitter
	f.mu.RUnlock()

	if ctx == nil || emitter == nil {
		return
	}
	emitter.Emit(ctx, EventName, event)
}

func (f *EventForwarder) LastSeq() int64 {
	return f.bus.LastSeq()
}

func (f *EventForwarder) Since(afterSeq int64) []api.Event {
	return f.bus.Since(afterSeq)
}
