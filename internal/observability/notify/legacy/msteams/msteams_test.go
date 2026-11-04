package msteams

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
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

// TestGetURL_MalformedWebhookURLIsNotQuoted verifies that GetURL rejects a
// webhook URL that does not parse without quoting it. The URL carries the
// webhook's credentials. Neither the error nor the debug log may contain it.
func TestGetURL_MalformedWebhookURLIsNotQuoted(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.DebugLevel)

	notifier, err := New(log, notifyConfig.Legacy{
		MSTeamsHook: "https://example.webhook.office.com/webhookb2/secrettoken%zz",
	})
	require.NoError(t, err)

	shoutrrrURL, err := notifier.GetURL()
	require.ErrorIs(t, err, errParseWebhookFailed)
	assert.NotContains(t, err.Error(), "secrettoken")
	assert.Empty(t, shoutrrrURL)
	assert.NotContains(t, buf.String(), "secrettoken")
}
