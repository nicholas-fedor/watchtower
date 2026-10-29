package notifications

import (
	"bytes"
	"net/url"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSlackTypeNotifier_GetURL_Channel verifies that a legacy Slack channel
// given as a Slack channel ID reaches the generated URL, and that any other
// channel keeps the webhook's own channel and is reported with a warning,
// since a shoutrrr Slack URL can only hold a channel ID.
func TestSlackTypeNotifier_GetURL_Channel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel string
		// wantChannel is the channel in the generated URL.
		wantChannel string
		wantWarning bool
	}{
		{name: "no channel", wantChannel: "webhook"},
		{name: "public channel ID", channel: "C8UJ12P4P", wantChannel: "C8UJ12P4P"},
		{name: "private channel ID", channel: "G0987654321", wantChannel: "G0987654321"},
		{name: "direct message ID", channel: "D024BE91L", wantChannel: "D024BE91L"},
		{name: "channel name", channel: "#ops", wantChannel: "webhook", wantWarning: true},
		{name: "bare channel name", channel: "ops", wantChannel: "webhook", wantWarning: true},
		{name: "user", channel: "@alice", wantChannel: "webhook", wantWarning: true},
		{name: "short uppercase word", channel: "DEV", wantChannel: "webhook", wantWarning: true},
		{name: "lowercase ID-like name", channel: "c8uj12p4p", wantChannel: "webhook", wantWarning: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			log := zerolog.New(&out).Level(zerolog.WarnLevel)
			notifier := &slackTypeNotifier{
				HookURL: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
				Channel: tt.channel,
				log:     &log,
			}

			raw, err := notifier.GetURL(nil)
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)

			assert.Equal(t, tt.wantChannel, parsed.Host)

			if tt.wantWarning {
				assert.Contains(t, out.String(), slackChannelNotConvertedMessage)
				assert.Contains(t, out.String(), tt.channel)
			} else {
				assert.Empty(t, out.String())
			}
		})
	}
}

// TestSlackTypeNotifier_GetURL_Discord verifies that a legacy Slack hook URL
// pointing at Discord's Slack-compatible endpoint becomes a shoutrrr Discord
// URL with the webhook ID and token, including a versioned API path, a trailing
// slash and a query string or fragment, whose contents never count as path
// segments. A URL without the webhook ID, token or /slack suffix, or with any
// other segment between /api and /webhooks, returns an error instead of
// panicking or building a wrong URL.
func TestSlackTypeNotifier_GetURL_Discord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hookURL string
		wantErr bool
	}{
		{name: "discord.com", hookURL: "https://discord.com/api/webhooks/123/abc/slack"},
		{name: "discordapp.com", hookURL: "https://discordapp.com/api/webhooks/123/abc/slack"},
		{name: "versioned API path", hookURL: "https://discord.com/api/v10/webhooks/123/abc/slack"},
		{name: "trailing slash", hookURL: "https://discord.com/api/webhooks/123/abc/slack/"},
		{name: "query string", hookURL: "https://discord.com/api/webhooks/123/abc/slack?wait=true"},
		{name: "query string with a slash", hookURL: "https://discord.com/api/webhooks/123/abc/slack?next=a/b"},
		{name: "trailing slash before a query string", hookURL: "https://discord.com/api/webhooks/123/abc/slack/?wait=true"},
		{name: "fragment", hookURL: "https://discord.com/api/webhooks/123/abc/slack#top"},
		{name: "host only", hookURL: "https://discord.com", wantErr: true},
		{name: "webhooks path only", hookURL: "https://discord.com/api/webhooks", wantErr: true},
		{name: "no slack suffix", hookURL: "https://discord.com/api/webhooks/123/abc", wantErr: true},
		{name: "empty webhook ID", hookURL: "https://discord.com/api/webhooks//abc/slack", wantErr: true},
		{name: "empty token", hookURL: "https://discord.com/api/webhooks/123//slack", wantErr: true},
		{name: "whitespace webhook ID", hookURL: "https://discord.com/api/webhooks/ \t/abc/slack", wantErr: true},
		{name: "whitespace token", hookURL: "https://discord.com/api/webhooks/123/  /slack", wantErr: true},
		{name: "not an API path", hookURL: "https://discord.com/x/webhooks/123/abc/slack", wantErr: true},
		{name: "non-version segment after api", hookURL: "https://discord.com/api/x/webhooks/123/abc/slack", wantErr: true},
		{name: "extra segments after api", hookURL: "https://discord.com/api/v10/x/webhooks/123/abc/slack", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			notifier := &slackTypeNotifier{HookURL: tt.hookURL, log: &log}

			raw, err := notifier.GetURL(nil)
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidDiscordWebhookURL)
				assert.Empty(t, raw)

				return
			}

			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, "discord", parsed.Scheme)
			assert.Equal(t, "abc", parsed.User.Username())
			assert.Equal(t, "123", parsed.Host)
		})
	}
}
