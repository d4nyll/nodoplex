package events

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ChangeAction represents the type of change that occurred.
type ChangeAction string

const (
	ActionCreated ChangeAction = "created"
	ActionUpdated ChangeAction = "updated"
	ActionDeleted ChangeAction = "deleted"
)

// ChangeEvent represents a single change to a node.
type ChangeEvent struct {
	ChangeID  int64           `json:"changeId"`
	Action    ChangeAction    `json:"action"`
	NodeType  string          `json:"nodeType"`
	NodeID    uuid.UUID       `json:"nodeId"`
	Node      json.RawMessage `json:"node,omitempty"`
	Previous  json.RawMessage `json:"previous,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// subscriber holds a channel and the node types it is interested in.
type subscriber struct {
	ch        chan ChangeEvent
	nodeTypes map[string]bool // empty means all types
}

// EventBus is a thread-safe in-memory pub/sub for change events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[uint64]*subscriber
	nextID      uint64
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[uint64]*subscriber),
	}
}

// Subscribe registers a new subscriber filtered by node types.
// If nodeTypes is empty, all events are received.
// Returns a read-only channel and an unsubscribe function.
func (b *EventBus) Subscribe(nodeTypes []string) (<-chan ChangeEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	typeFilter := make(map[string]bool, len(nodeTypes))
	for _, t := range nodeTypes {
		typeFilter[t] = true
	}

	sub := &subscriber{
		ch:        make(chan ChangeEvent, 64),
		nodeTypes: typeFilter,
	}
	b.subscribers[id] = sub

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers, id)
		close(sub.ch)
	}

	return sub.ch, unsubscribe
}

// Publish sends an event to all matching subscribers.
// Non-blocking: if a subscriber's channel is full, the event is dropped for that subscriber.
func (b *EventBus) Publish(event ChangeEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		if len(sub.nodeTypes) > 0 && !sub.nodeTypes[event.NodeType] {
			continue
		}
		// Non-blocking send
		select {
		case sub.ch <- event:
		default:
			// subscriber can't keep up; drop event
		}
	}
}
