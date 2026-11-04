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

// TestGotifyTypeNotifier_GetURL_TLSSkipVerify verifies that skipping TLS
// verification reaches the generated Gotify URL.
func TestGotifyTypeNotifier_GetURL_TLSSkipVerify(t *testing.T) {
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
			notifier := &gotifyTypeNotifier{
				gotifyURL:           "https://gotify.example.com",
				gotifyAppToken:      "Aaaaaaaaaaaaaaa",
				gotifySkipTLSVerify: tt.skipVerify,
				log:                 &log,
			}

			raw, err := notifier.GetURL()
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSkip, parsed.Query().Get("skiptlsverify"))
			assert.Equal(t, "gotify.example.com", parsed.Host)
		})
	}
}

// TestNew verifies that New builds a notifier from a valid server URL and
// token. It rejects an empty or non-HTTP server URL. It also rejects an empty
// token before it checks the URL. A rejected HTTP URL then logs no
// insecure-HTTP warning.
func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     notifyConfig.Legacy
		wantErr error
		// wantInsecureWarning is whether New warns that the URL uses plain HTTP.
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
			notifier, err := New(log, tt.cfg)

			warnings := logcapture.Messages(t, buf.String(), "warn")
			if tt.wantInsecureWarning {
				assert.Equal(t, []string{"Using an HTTP URL for Gotify is insecure"}, warnings)
			} else {
				assert.Empty(t, warnings)
			}

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

// TestGetURL_MalformedServerURLIsNotQuoted verifies that GetURL rejects a
// server URL that does not parse without quoting it. The URL can carry
// credentials. Neither the error nor the debug log may contain it.
func TestGetURL_MalformedServerURLIsNotQuoted(t *testing.T) {
	t.Parallel()

	log, buf := logcapture.NewJSON(zerolog.DebugLevel)

	notifier, err := New(log, notifyConfig.Legacy{
		GotifyURL:   "https://gotify.example.com:secrettoken",
		GotifyToken: "Aaaaaaaaaaaaaaa",
	})
	require.NoError(t, err)

	shoutrrrURL, err := notifier.GetURL()
	require.ErrorIs(t, err, errInvalidURL)
	assert.NotContains(t, err.Error(), "secrettoken")
	assert.Empty(t, shoutrrrURL)
	assert.NotContains(t, buf.String(), "secrettoken")
}
