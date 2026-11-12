package msteams

import (
	"net/url"
	"testing"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/teams"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// workflowHook is a signed Power Automate workflow webhook URL whose signature
// is "secrettoken".
const workflowHook = "https://default.environment.api.powerplatform.com/powerautomate/automations/direct" +
	"/workflows/abc123/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=secrettoken"

// TestURL verifies that URL converts the webhook URL to a Teams URL that
// Shoutrrr reads back with the same webhook URL and the legacy color.
func TestURL(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	raw, err := URL(&log, notifyConfig.Legacy{MSTeamsHook: workflowHook})
	require.NoError(t, err)

	serviceURL, err := url.Parse(raw)
	require.NoError(t, err)

	var config teams.Config
	require.NoError(t, config.SetURL(serviceURL))
	assert.Equal(t, workflowHook, config.Host)
	assert.Equal(t, legacy.ColorHex, config.Color)
}

// TestURL_NoWebhookURL verifies that URL rejects settings without a webhook
// URL.
func TestURL_NoWebhookURL(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	raw, err := URL(&log, notifyConfig.Legacy{MSTeamsHook: ""})
	require.ErrorIs(t, err, errEmptyWebhookURL)
	assert.Empty(t, raw)
}

// TestURL_TraceLogLeavesOutWebhook verifies that the trace-level log leaves out
// the webhook URL and the generated URL, which both carry the webhook's
// signature.
func TestURL_TraceLogLeavesOutWebhook(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.TraceLevel)

	_, err := URL(log, notifyConfig.Legacy{MSTeamsHook: workflowHook})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, logcapture.Messages(t, out, "trace"), "Generated Microsoft Teams service URL")
	assert.NotContains(t, out, "secrettoken")
}
