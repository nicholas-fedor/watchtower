package email

import (
	"net/url"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// validConfig returns legacy email settings that convert without an error.
// Each test changes the settings it covers.
//
// Returns:
//   - notifyConfig.Legacy: Settings with a from address, a to address, a
//     server and port 587.
func validConfig() notifyConfig.Legacy {
	return notifyConfig.Legacy{
		EmailFrom:   "watchtower@example.com",
		EmailTo:     "admin@example.com",
		EmailServer: "smtp.example.com",
		EmailPort:   587,
	}
}

// TestURL_TLSSkipVerify verifies that skipping TLS verification keeps the
// connection encrypted and only skips the certificate check.
func TestURL_TLSSkipVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		tlsSkipVerify bool
		// wantSkip is the expected skiptlsverify value, empty when absent.
		wantSkip string
	}{
		{name: "verification on", tlsSkipVerify: false, wantSkip: ""},
		{name: "verification skipped", tlsSkipVerify: true, wantSkip: "Yes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.EmailTLSSkipVerify = tt.tlsSkipVerify

			log := zerolog.Nop()
			raw, err := URL(&log, cfg)
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)

			query := parsed.Query()
			assert.Equal(t, "Auto", query.Get("encryption"))
			assert.Equal(t, "Yes", query.Get("usestarttls"))
			assert.Equal(t, tt.wantSkip, query.Get("skiptlsverify"))
		})
	}
}

// TestURL_Validation verifies that URL converts settings with a from address, a
// to address, a server and a port from 0 to 65535. Port 0 leaves the port out
// of the URL. Shoutrrr then uses the SMTP default. It rejects settings that
// lack a required setting or have a port outside that range.
func TestURL_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// edit changes the valid settings into the case's settings.
		edit func(cfg *notifyConfig.Legacy)
		// wantPort is the port in the converted URL, empty when it has none.
		wantPort string
		wantErr  error
	}{
		{name: "all required settings", edit: func(*notifyConfig.Legacy) {}, wantPort: "587"},
		{name: "lowest port", edit: func(cfg *notifyConfig.Legacy) { cfg.EmailPort = 0 }, wantPort: ""},
		{name: "highest port", edit: func(cfg *notifyConfig.Legacy) { cfg.EmailPort = 65535 }, wantPort: "65535"},
		{name: "no from address", edit: func(cfg *notifyConfig.Legacy) { cfg.EmailFrom = "" }, wantErr: errEmptyFrom},
		{name: "no to address", edit: func(cfg *notifyConfig.Legacy) { cfg.EmailTo = "" }, wantErr: errEmptyTo},
		{name: "no server", edit: func(cfg *notifyConfig.Legacy) { cfg.EmailServer = "" }, wantErr: errEmptyServer},
		{name: "negative port", edit: func(cfg *notifyConfig.Legacy) { cfg.EmailPort = -1 }, wantErr: errInvalidPortRange},
		{
			name:    "port above the range",
			edit:    func(cfg *notifyConfig.Legacy) { cfg.EmailPort = 65536 },
			wantErr: errInvalidPortRange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.edit(&cfg)

			log := zerolog.Nop()
			raw, err := URL(&log, cfg)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, raw)

				return
			}

			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, "smtp.example.com", parsed.Hostname())
			assert.Equal(t, tt.wantPort, parsed.Port())
		})
	}
}

// TestURL_Authentication verifies that a configured user turns on plain
// authentication. Without a user, the URL uses none.
func TestURL_Authentication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		user     string
		wantAuth string
	}{
		{name: "no user", wantAuth: "None"},
		{name: "user", user: "containrrrbot", wantAuth: "Plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.EmailUser = tt.user
			cfg.EmailPassword = "secret-password"

			log := zerolog.Nop()
			raw, err := URL(&log, cfg)
			require.NoError(t, err)

			parsed, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAuth, parsed.Query().Get("auth"))
		})
	}
}

// TestURL_TraceLogLeavesOutPassword verifies that the trace-level log reports
// that credentials are configured without the password or the generated URL,
// which holds it.
func TestURL_TraceLogLeavesOutPassword(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.EmailUser = "containrrrbot"
	cfg.EmailPassword = "secret-password"

	log, buf := logcapture.NewJSON(zerolog.TraceLevel)

	_, err := URL(log, cfg)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"credentials_configured":true`)
	assert.Contains(t, logcapture.Messages(t, out, "trace"), "Generated SMTP URL")
	assert.NotContains(t, out, "secret-password")
}
