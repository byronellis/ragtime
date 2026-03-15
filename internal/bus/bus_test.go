package bus

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/byronellis/ragtime/internal/protocol"
)

func TestSubscribeAndPublish(t *testing.T) {
	b := New()
	var got *protocol.HookEvent

	b.Subscribe(func(e *protocol.HookEvent) {
		got = e
	})

	event := &protocol.HookEvent{Agent: "claude", ToolName: "Read"}
	b.Publish(event)

	if got == nil {
		t.Fatal("subscriber was not called")
	}
	if got.Agent != "claude" {
		t.Errorf("agent = %q, want %q", got.Agent, "claude")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	var count atomic.Int32

	for i := 0; i < 5; i++ {
		b.Subscribe(func(e *protocol.HookEvent) {
			count.Add(1)
		})
	}

	b.Publish(&protocol.HookEvent{Agent: "test"})

	if c := count.Load(); c != 5 {
		t.Errorf("count = %d, want 5", c)
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New()
	var count atomic.Int32

	cancel := b.Subscribe(func(e *protocol.HookEvent) {
		count.Add(1)
	})

	b.Publish(&protocol.HookEvent{})
	if c := count.Load(); c != 1 {
		t.Fatalf("count = %d after first publish, want 1", c)
	}

	cancel()

	b.Publish(&protocol.HookEvent{})
	if c := count.Load(); c != 1 {
		t.Errorf("count = %d after unsubscribe+publish, want 1", c)
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	b := New()
	cancel := b.Subscribe(func(e *protocol.HookEvent) {})

	cancel()
	cancel() // should not panic
}

func TestConcurrentPublish(t *testing.T) {
	b := New()
	var count atomic.Int64

	b.Subscribe(func(e *protocol.HookEvent) {
		count.Add(1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish(&protocol.HookEvent{Agent: "test"})
		}()
	}
	wg.Wait()

	if c := count.Load(); c != 100 {
		t.Errorf("count = %d, want 100", c)
	}
}

func TestPublishNoSubscribers(t *testing.T) {
	b := New()
	// Should not panic
	b.Publish(&protocol.HookEvent{Agent: "test"})
}

func TestSubscribeDuringPublish(t *testing.T) {
	b := New()
	var called atomic.Bool

	// First subscriber adds a new subscriber during publish
	b.Subscribe(func(e *protocol.HookEvent) {
		b.Subscribe(func(e *protocol.HookEvent) {
			called.Store(true)
		})
	})

	// First publish triggers the first subscriber which adds the second
	b.Publish(&protocol.HookEvent{})

	// Second publish should reach the newly added subscriber
	b.Publish(&protocol.HookEvent{})

	if !called.Load() {
		t.Error("subscriber added during publish was not called on next publish")
	}
}
