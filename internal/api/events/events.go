package events

import (
	"sync"
	"time"
)

const (
	// subscriberChannelSize is the buffer size for each subscriber channel.
	subscriberChannelSize = 10
	// maxSubscribers is the maximum number of concurrent SSE subscribers.
	maxSubscribers = 100
)

// Event represents a Watchtower operational event emitted to SSE subscribers.
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// Broadcaster manages SSE subscriber registration and event distribution.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers []chan Event
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make([]chan Event, 0, maxSubscribers),
	}
}

// Subscribe registers a new subscriber and returns a channel for receiving events.
// The done channel should be closed when the subscriber wants to unsubscribe.
func (b *Broadcaster) Subscribe() <-chan Event {
	subCh := make(chan Event, subscriberChannelSize)

	b.mu.Lock()
	b.subscribers = append(b.subscribers, subCh)
	b.mu.Unlock()

	return subCh
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *Broadcaster) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)

			close(sub)

			return
		}
	}
}

// Publish sends an event to all registered subscribers.
// Subscribers that are full (backpressure) have their event dropped.
func (b *Broadcaster) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subscribers)
}
