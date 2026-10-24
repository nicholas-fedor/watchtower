package logcapture

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
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
