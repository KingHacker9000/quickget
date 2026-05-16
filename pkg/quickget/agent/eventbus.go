package agent

import (
	"log"
	"sync"
	"time"

	"quickget/pkg/quickget/events"
)

const defaultSubscriberBuffer = 128

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan events.Event]struct{}
	closed      bool
	debug       bool
	debugMu     sync.Mutex
	debugWindow eventBusDebugWindow
}

type eventBusDebugWindow struct {
	start             time.Time
	published         int64
	progressPublished int64
	subscriberWrites  int64
	dropped           int64
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan events.Event]struct{}),
		debug:       debugProgressEnabled(),
		debugWindow: eventBusDebugWindow{start: time.Now().UTC()},
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

	var writes int64
	var dropped int64
	for _, ch := range snapshot {
		select {
		case ch <- event:
			writes++
		default:
			// Drop events for slow subscribers to avoid blocking publishers.
			dropped++
		}
	}
	b.debugRecord(event, writes, dropped)
}

func (b *EventBus) debugRecord(event events.Event, writes int64, dropped int64) {
	if !b.debug {
		return
	}
	b.debugMu.Lock()
	defer b.debugMu.Unlock()
	if b.debugWindow.start.IsZero() {
		b.debugWindow.start = time.Now().UTC()
	}
	b.debugWindow.published++
	if event.Type == events.EventDownloadProgress {
		b.debugWindow.progressPublished++
	}
	b.debugWindow.subscriberWrites += writes
	b.debugWindow.dropped += dropped

	if time.Since(b.debugWindow.start) < time.Second {
		return
	}
	log.Printf(
		"agent: bus-debug published_per_sec=%d progress_published_per_sec=%d subscriber_writes_per_sec=%d dropped_per_sec=%d",
		b.debugWindow.published,
		b.debugWindow.progressPublished,
		b.debugWindow.subscriberWrites,
		b.debugWindow.dropped,
	)
	b.debugWindow = eventBusDebugWindow{start: time.Now().UTC()}
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
