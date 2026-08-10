package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

func TestNew(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	log := logging.New(buf, logging.InfoLevel)
	require.NotNil(t, log)

	log.Info().Msg("hello")
	log.Debug().Msg("hidden")

	var event map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &event))
	assert.Equal(t, "info", event["level"])
	assert.Equal(t, "hello", event["message"])
	assert.Contains(t, event, "time")
	assert.NotContains(t, buf.String(), "hidden")
}

func TestNew_LevelFilter(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	log := logging.New(buf, logging.WarnLevel)

	log.Info().Msg("info")
	log.Warn().Msg("warn")

	assert.NotContains(t, buf.String(), `"info"`)
	assert.Contains(t, buf.String(), "warn")
}

func TestWith(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	log := logging.New(buf, logging.InfoLevel)
	child := logging.With(log, "container", "nginx")

	require.NotNil(t, child)
	assert.NotSame(t, log, child)

	child.Info().Msg("started")

	var event map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &event))
	assert.Equal(t, "nginx", event["container"])
	assert.Equal(t, "started", event["message"])
}

func TestWithFields(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	log := logging.New(buf, logging.InfoLevel)
	child := logging.WithFields(log, map[string]any{
		"container": "redis",
		"id":        "abc",
	})

	child.Info().Msg("updated")

	var event map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &event))
	assert.Equal(t, "redis", event["container"])
	assert.Equal(t, "abc", event["id"])
}

func TestWithError(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	log := logging.New(buf, logging.InfoLevel)

	// nil error leaves logger unchanged (same pointer).
	same := logging.WithError(log, nil)
	assert.Same(t, log, same)

	err := errors.New("boom")
	child := logging.WithError(log, err)
	assert.NotSame(t, log, child)

	child.Error().Msg("failed")

	var event map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &event))
	assert.Equal(t, "boom", event["error"])
	assert.Equal(t, "failed", event["message"])
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    zerolog.Level
		wantErr bool
	}{
		{"panic", zerolog.PanicLevel, false},
		{"fatal", zerolog.FatalLevel, false},
		{"error", zerolog.ErrorLevel, false},
		{"warn", zerolog.WarnLevel, false},
		{"warning", zerolog.WarnLevel, false},
		{"WARNING", zerolog.WarnLevel, false},
		{"info", zerolog.InfoLevel, false},
		{"debug", zerolog.DebugLevel, false},
		{"trace", zerolog.TraceLevel, false},
		{" Trace ", zerolog.TraceLevel, false},
		{"", zerolog.NoLevel, true},
		{"verbose", zerolog.NoLevel, true},
		{"invalid", zerolog.NoLevel, true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := logging.ParseLevel(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.want, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfigureLevel(t *testing.T) {
	t.Parallel()

	newInfo := func() *zerolog.Logger {
		buf := &bytes.Buffer{}

		return logging.New(buf, logging.InfoLevel)
	}

	t.Run("trace flag overrides", func(t *testing.T) {
		t.Parallel()

		log := logging.ConfigureLevel(newInfo(), "error", "true", "true")
		assert.Equal(t, zerolog.TraceLevel, log.GetLevel())
	})

	t.Run("debug flag overrides raw level", func(t *testing.T) {
		t.Parallel()

		log := logging.ConfigureLevel(newInfo(), "error", "true", "")
		assert.Equal(t, zerolog.DebugLevel, log.GetLevel())
	})

	t.Run("raw level applied when no aliases", func(t *testing.T) {
		t.Parallel()

		log := logging.ConfigureLevel(newInfo(), "warn", "", "false")
		assert.Equal(t, zerolog.WarnLevel, log.GetLevel())
	})

	t.Run("empty raw level leaves logger unchanged", func(t *testing.T) {
		t.Parallel()

		base := newInfo()
		log := logging.ConfigureLevel(base, "", "", "")
		assert.Same(t, base, log)
		assert.Equal(t, zerolog.InfoLevel, log.GetLevel())
	})

	t.Run("invalid raw level leaves logger unchanged", func(t *testing.T) {
		t.Parallel()

		base := newInfo()
		log := logging.ConfigureLevel(base, "not-a-level", "0", "no")
		assert.Same(t, base, log)
		assert.Equal(t, zerolog.InfoLevel, log.GetLevel())
	})

	t.Run("trace wins over debug", func(t *testing.T) {
		t.Parallel()

		log := logging.ConfigureLevel(newInfo(), "info", "1", "yes")
		assert.Equal(t, zerolog.TraceLevel, log.GetLevel())
	})
}
