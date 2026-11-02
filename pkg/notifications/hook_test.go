package notifications

import (
	"io"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestRun_EventFieldMapPreservesLargeIntegers ensures large integer fields such as
// removed_orchestrators render exactly without float precision loss or scientific notation.
func TestRun_EventFieldMapPreservesLargeIntegers(t *testing.T) {
	t.Parallel()

	root := zerolog.New(io.Discard).Level(zerolog.TraceLevel).With().Timestamp().Logger()
	n := createTestNotifier(
		newTestLogger(t),
		[]string{},
		zerolog.TraceLevel,
		true,
		StaticData{},
		false,
		0,
	)
	n.RegisterHook(&root)
	n.StartNotification(true)

	// Value exceeds float64 exact-integer range (2^53) so json.Unmarshal float path would corrupt it.
	const largeCount int64 = 9007199254740993

	root.Info().
		Int64("removed_orchestrators", largeCount).
		Msg("cleanup summary")

	n.entriesMutex.RLock()
	entries := append([]*notificationEntry(nil), n.entries...)
	n.entriesMutex.RUnlock()

	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Data)

	got, ok := entries[0].Data["removed_orchestrators"].(int64)
	require.True(t, ok, "removed_orchestrators must be int64, got %T", entries[0].Data["removed_orchestrators"])
	require.Equal(t, largeCount, got)

	// Template must render the exact decimal digits (no scientific notation).
	const tpl = `{{ range .Entries }}{{ index .Data "removed_orchestrators" }}{{ end }}`

	notifier, err := createNotifierWithTemplate(t, tpl, false)
	require.NoError(t, err)

	rendered, err := notifier.buildMessage(notificationData{Entries: entries})
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", rendered)

	n.Close()
}

// TestRun_EventFieldMapPreservesApplicationFields verifies eventFieldMap through
// the notification emission path: a known application field on a zerolog event
// is retained on notificationEntry.Data. Guards against silent drops when
// parsing Event.buf fails (fail-closed would leave the queue empty).
func TestRun_EventFieldMapPreservesApplicationFields(t *testing.T) {
	t.Parallel()

	root := zerolog.New(io.Discard).Level(zerolog.TraceLevel).With().Timestamp().Logger()
	n := createTestNotifier(
		newTestLogger(t),
		[]string{},
		zerolog.TraceLevel,
		true,
		StaticData{},
		false,
		0,
	)
	n.RegisterHook(&root)
	n.StartNotification(true)

	const (
		wantContainer = "app-container-1"
		wantImage     = "nginx:latest"
		wantMsg       = "Found new image"
	)

	root.Info().
		Str("container", wantContainer).
		Str("image", wantImage).
		Msg(wantMsg)

	n.entriesMutex.RLock()
	entries := append([]*notificationEntry(nil), n.entries...)
	n.entriesMutex.RUnlock()

	require.Len(t, entries, 1, "event must be retained when Event.buf parses successfully")
	require.Equal(t, wantMsg, entries[0].Message)
	require.NotNil(t, entries[0].Data, "application fields must populate Data")
	require.Equal(t, wantContainer, entries[0].Data["container"])
	require.Equal(t, wantImage, entries[0].Data["image"])
	// Envelope keys are stripped by eventFieldMap.
	_, hasLevel := entries[0].Data[zerolog.LevelFieldName]
	_, hasTime := entries[0].Data[zerolog.TimestampFieldName]
	_, hasMessage := entries[0].Data[zerolog.MessageFieldName]

	require.False(t, hasLevel)
	require.False(t, hasTime)
	require.False(t, hasMessage)

	n.Close()
}

// TestRun_NotifyNoAndFailClosed verifies loop-prevention and fail-closed field extraction.
func TestRun_NotifyNoAndFailClosed(t *testing.T) {
	t.Parallel()

	n := createTestNotifier(
		newTestLogger(t),
		[]string{},
		zerolog.TraceLevel,
		true,
		StaticData{},
		false,
		0,
	)
	// Enable batching without starting the worker goroutine.
	n.entries = make([]*notificationEntry, 0, initialEntriesCapacity)

	// Fail-closed: nil event must not enqueue.
	n.Run(nil, zerolog.InfoLevel, "should not queue")
	n.entriesMutex.RLock()
	require.Empty(t, n.entries)
	n.entriesMutex.RUnlock()

	// Real zerolog path: notify=no child must not enqueue.
	root := zerolog.New(io.Discard).Level(zerolog.TraceLevel)
	n.RegisterHook(&root)
	// After RegisterHook the worker runs. Still batch via StartNotification.
	n.StartNotification(true)

	noLog := root.With().Str("notify", "no").Logger()
	noLog.Info().Msg("internal must not notify")

	n.entriesMutex.RLock()
	require.Empty(t, n.entries, "notify=no events must not be queued")
	n.entriesMutex.RUnlock()

	// Application log without notify=no should enqueue.
	root.Info().Str("container", "c1").Msg("Found new image")

	// Allow hook to run synchronously (hooks run in emitting goroutine).
	n.entriesMutex.RLock()
	count := len(n.entries)
	n.entriesMutex.RUnlock()
	require.Equal(t, 1, count, "app log without notify=no should queue")

	n.Close()
}

// TestRun_ConcurrentEnqueue verifies concurrent application logs are all queued
// (no global single-flight gate that would drop parallel Runs).
func TestRun_ConcurrentEnqueue(t *testing.T) {
	t.Parallel()

	root := zerolog.New(io.Discard).Level(zerolog.TraceLevel)
	n := createTestNotifier(
		newTestLogger(t),
		[]string{},
		zerolog.TraceLevel,
		true,
		StaticData{},
		false,
		0,
	)
	n.RegisterHook(&root)
	n.StartNotification(true)

	const workers = 32

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := range workers {
		go func(i int) {
			defer wg.Done()

			root.Info().Int("i", i).Msg("concurrent event")
		}(i)
	}

	wg.Wait()

	n.entriesMutex.RLock()
	count := len(n.entries)
	n.entriesMutex.RUnlock()
	require.Equal(t, workers, count, "every concurrent log must enqueue")

	n.Close()
}
