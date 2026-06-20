package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBroadcaster(t *testing.T) {
	b := NewBroadcaster()
	require.NotNil(t, b)
	assert.Equal(t, 0, b.SubscriberCount())
}

func TestSubscribe(t *testing.T) {
	b := NewBroadcaster()

	ch := b.Subscribe()
	require.NotNil(t, ch)
	assert.Equal(t, 1, b.SubscriberCount())
}

func TestUnsubscribe(t *testing.T) {
	b := NewBroadcaster()

	ch := b.Subscribe()
	assert.Equal(t, 1, b.SubscriberCount())

	b.Unsubscribe(ch)
	assert.Equal(t, 0, b.SubscriberCount())
}

func TestPublish(t *testing.T) {
	b := NewBroadcaster()

	ch := b.Subscribe()

	event := Event{
		Type:      "scan_completed",
		Timestamp: time.Now().UTC(),
		Data:      map[string]int{"scanned": 5},
	}

	b.Publish(event)

	select {
	case received := <-ch:
		assert.Equal(t, event.Type, received.Type)
		assert.Equal(t, event.Data, received.Data)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishMultipleSubscribers(t *testing.T) {
	b := NewBroadcaster()

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	event := Event{Type: "scan_started", Timestamp: time.Now().UTC()}
	b.Publish(event)

	select {
	case received := <-ch1:
		assert.Equal(t, event.Type, received.Type)
	case <-time.After(time.Second):
		t.Fatal("subscriber 1 timed out")
	}

	select {
	case received := <-ch2:
		assert.Equal(t, event.Type, received.Type)
	case <-time.After(time.Second):
		t.Fatal("subscriber 2 timed out")
	}
}

func TestPublishBackpressure(t *testing.T) {
	b := NewBroadcaster()

	ch := b.Subscribe()

	for range subscriberChannelSize + 5 {
		b.Publish(Event{Type: "test", Timestamp: time.Now().UTC()})
	}

	count := 0
	drainDone := time.After(100 * time.Millisecond)

	for {
		select {
		case <-ch:
			count++
		case <-drainDone:
			goto done
		}
	}

done:
	assert.LessOrEqual(t, count, subscriberChannelSize)
}

func TestUnsubscribeNonExistent(t *testing.T) {
	b := NewBroadcaster()

	ch := make(chan Event)
	b.Unsubscribe(ch)
	assert.Equal(t, 0, b.SubscriberCount())
}
