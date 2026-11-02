package notify

import (
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventFieldMap_NilEvent verifies that eventFieldMap reports no fields for
// a nil event.
func TestEventFieldMap_NilEvent(t *testing.T) {
	t.Parallel()

	fields, _, ok := eventFieldMap(nil)
	assert.False(t, ok)
	assert.Nil(t, fields)
}

// TestEventFieldMap_NumbersAndTimestamp verifies that eventFieldMap reads
// numeric fields from an event whose timestamp field is not a valid time.
func TestEventFieldMap_NumbersAndTimestamp(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	hook := zerolog.HookFunc(func(event *zerolog.Event, _ zerolog.Level, _ string) {
		fields, _, ok := eventFieldMap(event)
		if ok {
			captured = fields
		}
	})

	log := zerolog.New(io.Discard).Hook(hook)
	log.Info().
		Int64("count", 42).
		Float64("ratio", 1.5).
		Str(zerolog.TimestampFieldName, "not-a-timestamp").
		Msg("numbers")

	require.NotNil(t, captured)
	assert.Equal(t, int64(42), captured["count"])
	assert.InDelta(t, 1.5, captured["ratio"], 0.001)
}
