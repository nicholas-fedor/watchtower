package api

import (
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name               string
		logrusLogger       *logrus.Logger
		rateLimitPerMinute int
		wantNil            bool
	}{
		{
			name:               "default rate limit",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: 60,
			wantNil:            false,
		},
		{
			name:               "zero rate limit falls back to default",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: 0,
			wantNil:            false,
		},
		{
			name:               "negative rate limit falls back to default",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: -1,
			wantNil:            false,
		},
		{
			name:               "custom rate limit",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: 120,
			wantNil:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.logrusLogger, tt.rateLimitPerMinute, ProxyConfig{}, CORSConfig{})
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.IsType(t, &fiber.App{}, got)
			}
		})
	}
}
