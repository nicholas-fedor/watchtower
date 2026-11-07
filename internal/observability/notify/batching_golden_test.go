package notify

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/golden"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// batchEntryTime is the fixed timestamp of every batched log entry.
var batchEntryTime = time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)

// batchEntries returns the entries one update session queues when two
// containers, web1 and web2, share an image and a third, db, uses its own.
// Two more containers, cache1 and cache2, share an image whose update their
// cooldown holds back. Each entry carries the fields its emitting code
// attaches.
func batchEntries() []*format.Entry {
	entry := func(message string, data map[string]any) *format.Entry {
		return &format.Entry{Message: message, Data: data, Time: batchEntryTime, Level: "info"}
	}

	sharedImage := map[string]any{"image": "org/web:1.0", "new_id": "aaa111111111"}
	cooldown := map[string]any{"image": "org/cache:3", "cooldown": "1d", "eligible_in": "2h", "eligible_at": "2026-10-07T14:00:00Z"}

	return []*format.Entry{
		entry("Only checking containers in scope", map[string]any{"scope": "production"}),
		entry("Image is within cooldown period - not eligible for update", withContainer(cooldown, "cache1")),
		entry("Image is within cooldown period - not eligible for update", withContainer(cooldown, "cache2")),
		entry("Found new image", withContainer(sharedImage, "web1")),
		entry("Found new image", withContainer(sharedImage, "web2")),
		entry("Found new image", map[string]any{"container": "db", "image": "org/db:2", "new_id": "bbb222222222"}),
		entry("Found new Git revision",
			map[string]any{"container": "web1", "revision": "github.com/org/web@v1.1", "short_commit": "0123456"}),
		entry("Found new Git revision",
			map[string]any{"container": "web2", "revision": "github.com/org/web@v1.1", "short_commit": "0123456"}),
		entry("Stopping container", map[string]any{"container": "web1", "id": "111111111111"}),
		entry("Stopping container", map[string]any{"container": "web2", "id": "222222222222"}),
		entry("Started new container", map[string]any{"container": "web1", "new_id": "aaa111111111"}),
		entry("Started new container", map[string]any{"container": "web2", "new_id": "aaa111111111"}),
		entry("Removing image",
			map[string]any{"container_name": "web1", "image_name": "org/web:1.0", "image_id": "01d111111111"}),
		entry("Removing image",
			map[string]any{"container_name": "web2", "image_name": "org/web:1.0", "image_id": "01d111111111"}),
	}
}

// withContainer returns a copy of data with the container field set.
func withContainer(data map[string]any, container string) map[string]any {
	out := map[string]any{"container": container}
	maps.Copy(out, data)

	return out
}

// newBatchingNotifier returns a legacy-template notifier whose worker never
// starts, so every message it renders stays in its message channel. It is
// batching, as a notifier is from its creation until the startup message.
func newBatchingNotifier(t *testing.T) *Notifier {
	t.Helper()

	notifier := createTestNotifier(t, newTestLogger(t), []string{}, zerolog.TraceLevel, true, format.StaticData{}, 0)
	t.Cleanup(notifier.Close)

	notifier.batch = &batch{}

	return notifier
}

// startSessionWith opens an update session's batch on notifier and queues
// entries in it, as the session's log lines would.
//
// Returns:
//   - func(*state.Report): The session's send.
//   - func(): The session's end.
func startSessionWith(notifier *Notifier, entries []*format.Entry) (func(*state.Report), func()) {
	send, end := notifier.StartSessionNotification()

	notifier.entriesMutex.Lock()
	notifier.batch.entries = entries
	notifier.entriesMutex.Unlock()

	return send, end
}

// queuedEntries returns a copy of the entries in the notifier's open batch.
// It fails the test when no batch is open, so an empty result means the batch
// holds nothing.
//
// Returns:
//   - []*format.Entry: The queued entries.
func queuedEntries(t *testing.T, notifier *Notifier) []*format.Entry {
	t.Helper()

	notifier.entriesMutex.RLock()
	defer notifier.entriesMutex.RUnlock()

	require.NotNil(t, notifier.batch, "the notifier is batching")

	return slices.Clone(notifier.batch.entries)
}

// sentMessages drains the messages the notifier rendered.
func sentMessages(notifier *Notifier) []string {
	var messages []string

	for {
		select {
		case msg := <-notifier.messages:
			messages = append(messages, msg)
		default:
			return messages
		}
	}
}

// writeMessages appends each message to out under a numbered heading.
func writeMessages(out *strings.Builder, messages []string) {
	for i, msg := range messages {
		fmt.Fprintf(out, "--- message %d\n%s\n", i+1, strings.TrimRight(msg, "\n"))
	}
}

// TestNotificationBatching snapshots the messages sent for one batch of queued
// entries in each sending mode: grouped, where entries repeated across
// containers sharing an image are removed; focused on a single container's
// report, where only that container's entries are sent; and split by
// container, where each container gets its own message without removal.
func TestNotificationBatching(t *testing.T) {
	var out strings.Builder

	out.WriteString("=== grouped\n")

	// A session's end sends the entries no notification took.
	grouped := newBatchingNotifier(t)
	_, endGrouped := startSessionWith(grouped, batchEntries())

	endGrouped()
	writeMessages(&out, sentMessages(grouped))

	out.WriteString("\n=== focused on web1\n")

	session := reportfixture.UpdatedReport(modelfixture.Container(containerfixture.WithName("/web1")), "new_image_id")
	require.Equal(t, "web1", session.Updated()[0].Name())

	focused := newBatchingNotifier(t)
	sendFocused, _ := startSessionWith(focused, batchEntries())
	sendFocused(state.NewSingleContainerReport(session.Updated()[0], session))
	writeMessages(&out, sentMessages(focused))

	out.WriteString("--- still queued\n")

	for _, entry := range queuedEntries(t, focused) {
		fmt.Fprintf(&out, "%s %v\n", entry.Message, entry.Data)
	}

	// The messages are sent in map order, so they are sorted for the snapshot.
	out.WriteString("\n=== split by container (sorted)\n")

	split := newBatchingNotifier(t)
	_, endSplit := startSessionWith(split, batchEntries())
	split.splitByContainer = true

	endSplit()

	messages := sentMessages(split)
	slices.Sort(messages)
	writeMessages(&out, messages)

	require.Nil(t, split.batch, "splitting by container ends the batch")

	golden.Assert(t, "notification-batching", []byte(out.String()))
}
