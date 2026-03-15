package bus

import (
	"sync"

	"github.com/byronellis/ragtime/internal/protocol"
)

// Subscriber is a function that receives hook events.
type Subscriber func(event *protocol.HookEvent)

// Bus is a simple synchronous pub/sub event bus.
type Bus struct {
	mu          sync.RWMutex
	nextID      int
	subscribers map[int]Subscriber
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[int]Subscriber),
	}
}

// Subscribe registers a subscriber to receive events and returns an
// unsubscribe function that removes it.
func (b *Bus) Subscribe(fn Subscriber) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers, id)
	}
}

// Publish sends an event to all subscribers.
func (b *Bus) Publish(event *protocol.HookEvent) {
	b.mu.RLock()
	subs := make([]Subscriber, 0, len(b.subscribers))
	for _, fn := range b.subscribers {
		subs = append(subs, fn)
	}
	b.mu.RUnlock()

	for _, fn := range subs {
		fn(event)
	}
}
