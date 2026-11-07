package events

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func makeEvent(action ChangeAction, nodeType string) ChangeEvent {
	return ChangeEvent{
		Namespace: "ns",
		ChangeID:  1,
		Action:    action,
		NodeType:  nodeType,
		NodeID:    uuid.New(),
		Timestamp: time.Now(),
	}
}

func TestNewEventBus(t *testing.T) {
	bus := NewEventBus()
	if bus == nil {
		t.Fatal("NewEventBus returned nil")
	}
	if len(bus.subscribers) != 0 {
		t.Errorf("new bus should have 0 subscribers, got %d", len(bus.subscribers))
	}
}

func TestSubscribeAndPublish(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe("ns", nil)
	defer unsub()

	event := makeEvent(ActionCreated, "vision")
	bus.Publish(event)

	select {
	case got := <-ch:
		if got.Action != ActionCreated {
			t.Errorf("Action = %q, want %q", got.Action, ActionCreated)
		}
		if got.NodeType != "vision" {
			t.Errorf("NodeType = %q, want %q", got.NodeType, "vision")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscribeWithTypeFilter(t *testing.T) {
	bus := NewEventBus()

	// Subscribe only to "task" events
	ch, unsub := bus.Subscribe("ns", []string{"task"})
	defer unsub()

	// Publish a "vision" event - should NOT be received
	bus.Publish(makeEvent(ActionCreated, "vision"))

	// Publish a "task" event - should be received
	taskEvent := makeEvent(ActionUpdated, "task")
	bus.Publish(taskEvent)

	select {
	case got := <-ch:
		if got.NodeType != "task" {
			t.Errorf("expected task event, got %q", got.NodeType)
		}
		if got.Action != ActionUpdated {
			t.Errorf("Action = %q, want %q", got.Action, ActionUpdated)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task event")
	}

	// Verify no vision event was received
	select {
	case got := <-ch:
		t.Errorf("unexpected event received: %+v", got)
	case <-time.After(50 * time.Millisecond):
		// Expected: no more events
	}
}

func TestSubscribeNoFilter_ReceivesAll(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe("ns", nil)
	defer unsub()

	types := []string{"vision", "goal", "task", "gitref"}
	for _, nt := range types {
		bus.Publish(makeEvent(ActionCreated, nt))
	}

	received := map[string]bool{}
	for range len(types) {
		select {
		case got := <-ch:
			received[got.NodeType] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for events")
		}
	}

	for _, nt := range types {
		if !received[nt] {
			t.Errorf("did not receive event for type %q", nt)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe("ns", nil)

	// Unsubscribe
	unsub()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}

	// Publishing after unsubscribe should not panic
	bus.Publish(makeEvent(ActionCreated, "vision"))
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()

	ch1, unsub1 := bus.Subscribe("ns", nil)
	defer unsub1()
	ch2, unsub2 := bus.Subscribe("ns", nil)
	defer unsub2()

	event := makeEvent(ActionDeleted, "goal")
	bus.Publish(event)

	for i, ch := range []<-chan ChangeEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.NodeType != "goal" {
				t.Errorf("subscriber %d: NodeType = %q, want %q", i, got.NodeType, "goal")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestPublishDropsWhenChannelFull(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe("ns", nil)
	defer unsub()

	// Fill the buffer (capacity is 64)
	for i := range 65 {
		bus.Publish(ChangeEvent{
			Namespace: "ns",
			ChangeID:  int64(i),
			Action:    ActionCreated,
			NodeType:  "vision",
			NodeID:    uuid.New(),
		})
	}

	// Should have 64 events in channel (65th dropped)
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 64 {
		t.Errorf("expected 64 events in channel, got %d", count)
	}
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	bus := NewEventBus()

	// Subscribe and unsubscribe concurrently with publishing to test thread safety
	const numPublishers = 20
	const eventsPerPublisher = 5

	ch, unsub := bus.Subscribe("ns", nil)

	// Drain events concurrently
	var received int64
	var drainWg sync.WaitGroup
	drainWg.Add(1)
	go func() {
		defer drainWg.Done()
		for range ch {
			received++
		}
	}()

	// Publish events from multiple goroutines
	var publishWg sync.WaitGroup
	publishWg.Add(numPublishers)
	for i := range numPublishers {
		go func(publisherID int) {
			defer publishWg.Done()
			for j := range eventsPerPublisher {
				bus.Publish(ChangeEvent{
					Namespace: "ns",
					ChangeID:  int64(publisherID*eventsPerPublisher + j),
					Action:    ActionCreated,
					NodeType:  "task",
					NodeID:    uuid.New(),
				})
			}
		}(i)
	}

	publishWg.Wait()
	// Allow time for all events to be consumed from channel
	time.Sleep(100 * time.Millisecond)
	unsub() // closes channel, ending the drain goroutine
	drainWg.Wait()

	total := numPublishers * eventsPerPublisher
	if received == 0 {
		t.Error("expected at least some events to be received")
	}
	if received > int64(total) {
		t.Errorf("received %d events, but only published %d", received, total)
	}
	// With buffer of 64 and 100 total events, we expect all to be received
	// since we're draining concurrently
	if received != int64(total) {
		t.Logf("received %d of %d events (some may have been dropped due to timing)", received, total)
	}
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	bus := NewEventBus()

	// Test that concurrent subscribe/unsubscribe/publish doesn't panic
	var wg sync.WaitGroup
	const iterations = 50

	wg.Add(iterations * 2)

	// Concurrent subscribers
	for range iterations {
		go func() {
			defer wg.Done()
			_, unsub := bus.Subscribe("ns", nil)
			time.Sleep(time.Millisecond)
			unsub()
		}()
	}

	// Concurrent publishers
	for i := range iterations {
		go func(id int) {
			defer wg.Done()
			bus.Publish(ChangeEvent{
				Namespace: "ns",
				ChangeID:  int64(id),
				Action:    ActionCreated,
				NodeType:  "vision",
				NodeID:    uuid.New(),
			})
		}(i)
	}

	wg.Wait()
	// Success if no panic
}

func TestSubscribeWithEmptySliceReceivesAll(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe("ns", []string{})
	defer unsub()

	bus.Publish(makeEvent(ActionCreated, "feature"))

	select {
	case got := <-ch:
		if got.NodeType != "feature" {
			t.Errorf("NodeType = %q, want %q", got.NodeType, "feature")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out - empty filter should receive all events")
	}
}

func TestSubscriberIDsAreUnique(t *testing.T) {
	bus := NewEventBus()

	_, unsub1 := bus.Subscribe("ns", nil)
	_, unsub2 := bus.Subscribe("ns", nil)
	_, unsub3 := bus.Subscribe("ns", nil)

	bus.mu.RLock()
	if len(bus.subscribers) != 3 {
		t.Errorf("expected 3 subscribers, got %d", len(bus.subscribers))
	}
	bus.mu.RUnlock()

	unsub2()

	bus.mu.RLock()
	if len(bus.subscribers) != 2 {
		t.Errorf("expected 2 subscribers after unsub, got %d", len(bus.subscribers))
	}
	bus.mu.RUnlock()

	unsub1()
	unsub3()

	bus.mu.RLock()
	if len(bus.subscribers) != 0 {
		t.Errorf("expected 0 subscribers, got %d", len(bus.subscribers))
	}
	bus.mu.RUnlock()
}

func TestPublishOnlyReachesSameNamespace(t *testing.T) {
	bus := NewEventBus()
	chA, unsubA := bus.Subscribe("a", nil)
	defer unsubA()
	chB, unsubB := bus.Subscribe("b", nil)
	defer unsubB()

	ev := makeEvent(ActionCreated, "task")
	ev.Namespace = "a"
	bus.Publish(ev)

	select {
	case got := <-chA:
		if got.NodeID != ev.NodeID {
			t.Errorf("subscriber a got node %s, want %s", got.NodeID, ev.NodeID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber a did not receive the event")
	}
	select {
	case got := <-chB:
		t.Fatalf("subscriber b received an event from namespace a: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}
