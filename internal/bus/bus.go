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
	subscribers []Subscriber
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{}
}

// Subscribe registers a subscriber to receive events.
func (b *Bus) Subscribe(fn Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, fn)
}

// Publish sends an event to all subscribers.
func (b *Bus) Publish(event *protocol.HookEvent) {
	b.mu.RLock()
	subs := b.subscribers
	b.mu.RUnlock()

	for _, fn := range subs {
		fn(event)
	}
}
