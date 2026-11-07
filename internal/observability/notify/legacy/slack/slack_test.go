package slack

import (
	"net/url"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// slackHook is a Slack webhook URL whose token Shoutrrr accepts.
const slackHook = "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"

// TestURL_Channel verifies that a legacy Slack channel
// given as a Slack channel ID reaches the generated URL, and that any other
// channel keeps the webhook's own channel and is reported with a warning,
// since a shoutrrr Slack URL can only hold a channel ID.
func TestURL_Channel(t *testing.T) {
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

			log, buf := logcapture.NewJSON(zerolog.WarnLevel)

			raw, err := URL(log, notifyConfig.Legacy{SlackHookURL: slackHook, SlackChannel: tt.channel})
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)

			assert.Equal(t, tt.wantChannel, parsed.Host)

			if tt.wantWarning {
				assert.Equal(t, []string{slackChannelNotConvertedMessage}, logcapture.Messages(t, buf.String(), "warn"))
				assert.Contains(t, buf.String(), tt.channel)
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

// TestURL_Discord verifies that a legacy Slack hook URL
// pointing at Discord's Slack-compatible endpoint becomes a shoutrrr Discord
// URL with the webhook ID and token, including a versioned API path, a trailing
// slash and a query string or fragment, whose contents never count as path
// segments. A URL without the webhook ID, token or /slack suffix, or with any
// other segment between /api and /webhooks, returns an error instead of
// panicking or building a wrong URL.
func TestURL_Discord(t *testing.T) {
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
		{name: "encoded whitespace webhook ID", hookURL: "https://discord.com/api/webhooks/%20%09/abc/slack", wantErr: true},
		{name: "encoded whitespace token", hookURL: "https://discord.com/api/webhooks/123/%20/slack", wantErr: true},
		{name: "malformed escape in token", hookURL: "https://discord.com/api/webhooks/123/%zz/slack", wantErr: true},
		{name: "not an API path", hookURL: "https://discord.com/x/webhooks/123/abc/slack", wantErr: true},
		{name: "non-version segment after api", hookURL: "https://discord.com/api/x/webhooks/123/abc/slack", wantErr: true},
		{name: "extra segments after api", hookURL: "https://discord.com/api/v10/x/webhooks/123/abc/slack", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()

			raw, err := URL(&log, notifyConfig.Legacy{SlackHookURL: tt.hookURL})
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

// TestURL_RequiredSettings verifies that URL rejects settings without a hook
// URL and converts one that is set.
func TestURL_RequiredSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     notifyConfig.Legacy
		wantErr error
	}{
		{
			name: "hook URL",
			cfg:  notifyConfig.Legacy{SlackHookURL: slackHook},
		},
		{
			name:    "no hook URL",
			cfg:     notifyConfig.Legacy{},
			wantErr: errEmptyHookURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			raw, err := URL(&log, tt.cfg)

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

// TestURL_Icon verifies how the icon settings reach the generated URL. A Slack
// URL takes the icon URL, or the emoji when no icon URL is set. A Discord URL
// takes the icon URL as its avatar.
func TestURL_Icon(t *testing.T) {
	t.Parallel()

	const (
		discordHook = "https://discord.com/api/webhooks/123/abc/slack"
		iconURL     = "https://example.com/icon.png"
	)

	tests := []struct {
		name  string
		cfg   notifyConfig.Legacy
		param string
		want  string
	}{
		{
			name:  "Slack icon URL",
			cfg:   notifyConfig.Legacy{SlackHookURL: slackHook, SlackIconURL: iconURL},
			param: "icon",
			want:  iconURL,
		},
		{
			name:  "Slack emoji",
			cfg:   notifyConfig.Legacy{SlackHookURL: slackHook, SlackIconEmoji: "whale"},
			param: "icon",
			want:  "whale",
		},
		{
			name:  "Slack icon URL wins over the emoji",
			cfg:   notifyConfig.Legacy{SlackHookURL: slackHook, SlackIconURL: iconURL, SlackIconEmoji: "whale"},
			param: "icon",
			want:  iconURL,
		},
		{
			name:  "Discord avatar",
			cfg:   notifyConfig.Legacy{SlackHookURL: discordHook, SlackIconURL: iconURL},
			param: "avatar",
			want:  iconURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			raw, err := URL(&log, tt.cfg)
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Query().Get(tt.param))
		})
	}
}

// TestURL_InvalidSlackToken verifies that a Slack hook URL whose token is not
// in Slack's three-part form returns an error.
func TestURL_InvalidSlackToken(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	raw, err := URL(&log, notifyConfig.Legacy{SlackHookURL: "https://hooks.slack.com/services/AAA/BBB/CCC"})

	require.ErrorContains(t, err, "failed to set Slack webhook token")
	assert.Empty(t, raw)
}

// TestURL_TraceLogLeavesOutToken verifies that the trace-level log leaves out
// the webhook token of both a Slack and a Discord hook URL.
func TestURL_TraceLogLeavesOutToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hook    string
		token   string
		message string
	}{
		{
			name:    "Slack",
			hook:    "https://hooks.slack.com/services/T00000000/B00000000/SECRETSECRETSECRETSECRET",
			token:   "SECRETSECRETSECRETSECRET",
			message: "Generated Slack service URL",
		},
		{
			name:    "Discord",
			hook:    "https://discord.com/api/webhooks/123/secrettoken/slack",
			token:   "secrettoken",
			message: "Generated Discord service URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, buf := logcapture.NewJSON(zerolog.TraceLevel)

			_, err := URL(log, notifyConfig.Legacy{SlackHookURL: tt.hook})
			require.NoError(t, err)

			out := buf.String()
			assert.Contains(t, logcapture.Messages(t, out, "trace"), tt.message)
			assert.NotContains(t, out, tt.token)
		})
	}
}
