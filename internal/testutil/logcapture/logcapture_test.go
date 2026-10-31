package logcapture

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew verifies that the logger writes logfmt lines at or above its level
// and drops the ones below it.
func TestNew(t *testing.T) {
	t.Parallel()

	log, out := New(zerolog.InfoLevel)

	log.Debug().Msg("hidden")
	log.Info().Str("container", "app").Msg("Renamed container")

	assert.NotContains(t, out.String(), "hidden")
	assert.Contains(t, out.String(), "Renamed container")
	assert.Contains(t, out.String(), "container=app")
}

// TestBuffer_ConcurrentWrites verifies that goroutines can write to the buffer
// while it is read, with every write kept.
func TestBuffer_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	var (
		buf Buffer
		wg  sync.WaitGroup
	)

	for range 50 {
		wg.Go(func() {
			_, _ = buf.Write([]byte("x"))
			_ = buf.String()
		})
	}

	wg.Wait()

	assert.Len(t, buf.String(), 50)
}

// TestNewJSON verifies that the JSON logger writes one decodable entry per
// line at or above its level, with a timestamp, and drops the ones below it.
func TestNewJSON(t *testing.T) {
	t.Parallel()

	log, out := NewJSON(zerolog.InfoLevel)

	log.Debug().Msg("hidden")
	log.Info().Str("container", "app").Msg("Renamed container")

	entries := Entries[Entry](t, out.String())
	require.Len(t, entries, 1)
	assert.Equal(t, "Renamed container", entries[0]["message"])
	assert.Equal(t, "app", entries[0]["container"])
	assert.Contains(t, entries[0], "time")
}

// TestEntries verifies that entries decode in log order into a field map or
// into a struct with only the fields a test reads, and that empty output has
// no entries.
func TestEntries(t *testing.T) {
	t.Parallel()

	log, out := NewJSON(zerolog.DebugLevel)

	log.Debug().Strs("links", []string{"db", "cache"}).Msg("first")
	log.Warn().Msg("second")

	maps := Entries[Entry](t, out.String())
	require.Len(t, maps, 2)
	assert.Equal(t, "first", maps[0]["message"])
	assert.Equal(t, "warn", maps[1]["level"])

	type linksEntry struct {
		Message string   `json:"message"`
		Links   []string `json:"links"`
	}

	structs := Entries[linksEntry](t, out.String())
	assert.Equal(t, []linksEntry{{Message: "first", Links: []string{"db", "cache"}}, {Message: "second"}}, structs)

	assert.Empty(t, Entries[Entry](t, ""))
}

// TestLevels verifies that only the entries with the given message report
// their levels, in log order.
func TestLevels(t *testing.T) {
	t.Parallel()

	log, out := NewJSON(zerolog.DebugLevel)

	log.Warn().Msg("Invalid label")
	log.Info().Msg("Other")
	log.Debug().Msg("Invalid label")

	assert.Equal(t, []string{"warn", "debug"}, Levels(t, out.String(), "Invalid label"))
	assert.Empty(t, Levels(t, out.String(), "Missing"))
}

// TestMessages verifies that only the entries at the given level report their
// messages, in log order.
func TestMessages(t *testing.T) {
	t.Parallel()

	log, out := NewJSON(zerolog.DebugLevel)

	log.Warn().Msg("first warning")
	log.Debug().Msg("detail")
	log.Warn().Msg("second warning")

	assert.Equal(t, []string{"first warning", "second warning"}, Messages(t, out.String(), "warn"))
	assert.Empty(t, Messages(t, out.String(), "error"))
}
