package msteams

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// TestURL_Validation verifies that URL converts an absolute https webhook URL.
// It rejects settings without a webhook URL and any other form of URL.
func TestURL_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hook    string
		wantErr error
	}{
		{
			name: "https URL",
			hook: "https://prod-00.westus.logic.azure.com:443/workflows/abc123/triggers/manual/paths/invoke",
		},
		{name: "no webhook URL", hook: "", wantErr: errEmptyWebhookURL},
		{name: "http URL", hook: "http://example.webhook.office.com/webhookb2/abc", wantErr: errParseWebhookFailed},
		{name: "relative URL", hook: "/webhookb2/abc", wantErr: errParseWebhookFailed},
		{name: "https URL without a host", hook: "https:///webhookb2/abc", wantErr: errParseWebhookFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			raw, err := URL(&log, notifyConfig.Legacy{MSTeamsHook: tt.hook})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, raw)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, raw)
		})
	}
}

// TestURL_MalformedWebhookURLIsNotQuoted verifies that URL rejects a webhook
// URL that does not parse without quoting it. The URL carries the webhook's
// credentials. Neither the error nor the debug log may contain it.
func TestURL_MalformedWebhookURLIsNotQuoted(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.DebugLevel)

	shoutrrrURL, err := URL(log, notifyConfig.Legacy{
		MSTeamsHook: "https://example.webhook.office.com/webhookb2/secrettoken%zz",
	})
	require.ErrorIs(t, err, errParseWebhookFailed)
	assert.NotContains(t, err.Error(), "secrettoken")
	assert.Empty(t, shoutrrrURL)
	assert.NotContains(t, buf.String(), "secrettoken")
}

// TestURL_TraceLogLeavesOutWebhook verifies that the trace-level log leaves out
// the webhook URL and the generated URL, which both carry the webhook's
// credentials.
func TestURL_TraceLogLeavesOutWebhook(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.TraceLevel)

	_, err := URL(log, notifyConfig.Legacy{
		MSTeamsHook: "https://example.webhook.office.com/webhookb2/secrettoken",
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, logcapture.Messages(t, out, "trace"), "Generated Microsoft Teams service URL")
	assert.NotContains(t, out, "secrettoken")
}
