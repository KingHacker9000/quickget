package agent

import (
	"sync"

	"quickget/pkg/quickget/events"
)

const defaultSubscriberBuffer = 16

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan events.Event]struct{}
	closed      bool
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan events.Event]struct{}),
	}
}

func (b *EventBus) Subscribe() (<-chan events.Event, func()) {
	ch := make(chan events.Event, defaultSubscriberBuffer)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subscribers[ch]; ok {
				delete(b.subscribers, ch)
				close(ch)
			}
			b.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

func (b *EventBus) Publish(event events.Event) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}

	snapshot := make([]chan events.Event, 0, len(b.subscribers))
	for ch := range b.subscribers {
		snapshot = append(snapshot, ch)
	}
	b.mu.RUnlock()

	for _, ch := range snapshot {
		select {
		case ch <- event:
		default:
			// Drop events for slow subscribers to avoid blocking publishers.
		}
	}
}

func (b *EventBus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	for ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, ch)
	}
	b.mu.Unlock()
}
