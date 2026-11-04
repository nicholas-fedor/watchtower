package msteams

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
)

// TestNew verifies that New rejects settings without a webhook URL and builds
// a notifier when one is set.
func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     notifyConfig.Legacy
		wantErr error
	}{
		{
			name: "webhook URL",
			cfg: notifyConfig.Legacy{
				MSTeamsHook: "https://prod-00.westus.logic.azure.com:443/workflows/abc123/triggers/manual/paths/invoke",
			},
		},
		{
			name:    "no webhook URL",
			cfg:     notifyConfig.Legacy{},
			wantErr: errEmptyWebhookURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			notifier, err := New(&log, tt.cfg)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, notifier)

				return
			}

			require.NoError(t, err)
			assert.NotNil(t, notifier)
		})
	}
}
