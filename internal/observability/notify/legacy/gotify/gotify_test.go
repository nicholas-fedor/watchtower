package gotify

import (
	"net/url"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// TestURL_TLSSkipVerify verifies that skipping TLS verification reaches the
// generated Gotify URL.
func TestURL_TLSSkipVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		skipVerify bool
		// wantSkip is the expected skiptlsverify value, empty when absent.
		wantSkip string
	}{
		{name: "verification on", skipVerify: false, wantSkip: ""},
		{name: "verification skipped", skipVerify: true, wantSkip: "Yes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			raw, err := URL(&log, notifyConfig.Legacy{
				GotifyURL:           "https://gotify.example.com",
				GotifyToken:         "Aaaaaaaaaaaaaaa",
				GotifyTLSSkipVerify: tt.skipVerify,
			})
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSkip, parsed.Query().Get("skiptlsverify"))
			assert.Equal(t, "gotify.example.com", parsed.Host)
		})
	}
}

// TestURL_RequiredSettings verifies that URL converts a valid server URL and
// token. It rejects a server URL that is empty or not HTTP. It also rejects one
// that does not parse or has no host name. An empty token is rejected before
// the URL is checked. A rejected HTTP URL logs no insecure-HTTP warning.
func TestURL_RequiredSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     notifyConfig.Legacy
		wantErr error
		// wantInsecureWarning is whether URL warns that the server URL uses plain HTTP.
		wantInsecureWarning bool
	}{
		{
			name: "HTTPS URL and token",
			cfg:  notifyConfig.Legacy{GotifyURL: "https://gotify.example.com", GotifyToken: "Aaaaaaaaaaaaaaa"},
		},
		{
			name:                "HTTP URL and token",
			cfg:                 notifyConfig.Legacy{GotifyURL: "http://gotify.example.com", GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantInsecureWarning: true,
		},
		{
			name:    "no URL",
			cfg:     notifyConfig.Legacy{GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantErr: errEmptyURL,
		},
		{
			name:    "URL without a scheme",
			cfg:     notifyConfig.Legacy{GotifyURL: "gotify.example.com", GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantErr: errURLScheme,
		},
		{
			name:    "HTTP URL that does not parse",
			cfg:     notifyConfig.Legacy{GotifyURL: "http://gotify.example.com:port", GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantErr: errInvalidURL,
		},
		{
			name:    "HTTP URL without a host",
			cfg:     notifyConfig.Legacy{GotifyURL: "http://", GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantErr: errInvalidURL,
		},
		{
			name:    "HTTP URL with only a port",
			cfg:     notifyConfig.Legacy{GotifyURL: "http://:8080", GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantErr: errInvalidURL,
		},
		{
			name:    "HTTPS URL with only a path",
			cfg:     notifyConfig.Legacy{GotifyURL: "https:///gotify", GotifyToken: "Aaaaaaaaaaaaaaa"},
			wantErr: errInvalidURL,
		},
		{
			name:    "no token",
			cfg:     notifyConfig.Legacy{GotifyURL: "https://gotify.example.com"},
			wantErr: errEmptyToken,
		},
		{
			name:    "HTTP URL without a token",
			cfg:     notifyConfig.Legacy{GotifyURL: "http://gotify.example.com"},
			wantErr: errEmptyToken,
		},
		{
			name:    "neither URL nor token",
			cfg:     notifyConfig.Legacy{},
			wantErr: errEmptyToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, buf := logcapture.NewJSON(zerolog.WarnLevel)
			raw, err := URL(log, tt.cfg)

			warnings := logcapture.Messages(t, buf.String(), "warn")
			if tt.wantInsecureWarning {
				assert.Equal(t, []string{"Using an HTTP URL for Gotify is insecure"}, warnings)
			} else {
				assert.Empty(t, warnings)
			}

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

// TestURL_MalformedServerURLIsNotQuoted verifies that URL rejects a server URL
// that does not parse without quoting it. The URL can carry credentials.
// Neither the error nor the debug log may contain it.
func TestURL_MalformedServerURLIsNotQuoted(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.DebugLevel)

	shoutrrrURL, err := URL(log, notifyConfig.Legacy{
		GotifyURL:   "https://gotify.example.com:secrettoken",
		GotifyToken: "Aaaaaaaaaaaaaaa",
	})
	require.ErrorIs(t, err, errInvalidURL)
	assert.NotContains(t, err.Error(), "secrettoken")
	assert.Empty(t, shoutrrrURL)
	assert.NotContains(t, buf.String(), "secrettoken")
}

// TestURL_TraceLogLeavesOutToken verifies that the trace-level log reports the
// token's length without the token or the generated URL, which holds it.
func TestURL_TraceLogLeavesOutToken(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.TraceLevel)

	_, err := URL(log, notifyConfig.Legacy{
		GotifyURL:   "https://gotify.example.com",
		GotifyToken: "Asecrettoken123",
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"token_length":15`)
	assert.Contains(t, logcapture.Messages(t, out, "trace"), "Generated Gotify service URL")
	assert.NotContains(t, out, "Asecrettoken123")
}
