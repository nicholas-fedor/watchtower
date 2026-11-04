package email

import (
	"net/url"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
)

// TestEmailTypeNotifier_GetURL_TLSSkipVerify verifies that skipping TLS
// verification keeps the connection encrypted and only skips the certificate
// check.
func TestEmailTypeNotifier_GetURL_TLSSkipVerify(t *testing.T) {
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

			log := zerolog.Nop()
			notifier := &emailTypeNotifier{
				From:          "watchtower@example.com",
				To:            "admin@example.com",
				Server:        "smtp.example.com",
				Port:          587,
				tlsSkipVerify: tt.tlsSkipVerify,
				log:           &log,
			}

			raw, err := notifier.GetURL()
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

// TestNew verifies that New builds a notifier when the from address, the to
// address and the server are set. It rejects settings that lack any of them.
func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     notifyConfig.Legacy
		wantErr error
	}{
		{
			name: "all required settings",
			cfg:  notifyConfig.Legacy{EmailFrom: "from@example.com", EmailTo: "to@example.com", EmailServer: "mail.example.com"},
		},
		{
			name:    "no from address",
			cfg:     notifyConfig.Legacy{EmailTo: "to@example.com", EmailServer: "mail.example.com"},
			wantErr: errEmptyFrom,
		},
		{
			name:    "no to address",
			cfg:     notifyConfig.Legacy{EmailFrom: "from@example.com", EmailServer: "mail.example.com"},
			wantErr: errEmptyTo,
		},
		{
			name:    "no server",
			cfg:     notifyConfig.Legacy{EmailFrom: "from@example.com", EmailTo: "to@example.com"},
			wantErr: errEmptyServer,
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
