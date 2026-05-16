package agent

import (
	"reflect"
	"testing"
	"time"

	"quickget/pkg/quickget/events"
)

func TestEventBusSubscriberReceivesPublishedEvent(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)

	ch, _ := bus.Subscribe()
	event := events.Event{Type: events.EventAgentReady, ID: "a"}
	bus.Publish(event)

	select {
	case got := <-ch:
		if !reflect.DeepEqual(got, event) {
			t.Fatalf("unexpected event: got %+v want %+v", got, event)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBusUnsubscribeStopsReceiving(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)

	ch, unsubscribe := bus.Subscribe()
	unsubscribe()

	if _, ok := <-ch; ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}

	bus.Publish(events.Event{Type: events.EventDownloadProgress, ID: "b"})
}

func TestEventBusMultipleSubscribersReceiveEvents(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)

	ch1, _ := bus.Subscribe()
	ch2, _ := bus.Subscribe()
	event := events.Event{Type: events.EventDownloadStarted, ID: "c"}

	bus.Publish(event)

	for i, ch := range []<-chan events.Event{ch1, ch2} {
		select {
		case got := <-ch:
			if !reflect.DeepEqual(got, event) {
				t.Fatalf("subscriber %d unexpected event: got %+v want %+v", i+1, got, event)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("subscriber %d timed out waiting for event", i+1)
		}
	}
}

func TestEventBusSlowSubscriberDoesNotBlockPublish(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)

	ch, _ := bus.Subscribe()
	_ = ch

	event := events.Event{Type: events.EventDownloadProgress, ID: "d"}
	for i := 0; i < defaultSubscriberBuffer; i++ {
		bus.Publish(event)
	}

	done := make(chan struct{})
	go func() {
		bus.Publish(event)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish blocked on slow subscriber")
	}
}
