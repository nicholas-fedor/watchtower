package auth

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_registryClient_Do(t *testing.T) {
	tests := []struct {
		name       string
		client     *registryClient
		req        *http.Request
		wantBody   string
		wantStatus int
		wantErr    bool
	}{
		{
			name: "successful request returns response",
			client: &registryClient{
				client: &http.Client{
					Transport: &mockRoundTripper{
						response: &http.Response{
							StatusCode: http.StatusOK,
							Body:       http.NoBody,
						},
					},
				},
			},
			req:        mustNewRequest(t, "http://example.com/v2/"),
			wantStatus: http.StatusOK,
		},
		{
			name: "request failure returns error",
			client: &registryClient{
				client: &http.Client{
					Transport: &mockRoundTripper{
						err: assert.AnError,
					},
				},
			},
			req:     mustNewRequest(t, "http://example.com/v2/"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.client.Do(tt.req)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			resp.Body.Close()
		})
	}
}

func Test_configureTLS(t *testing.T) {
	const tlsMinVersionKey = "WATCHTOWER_REGISTRY_TLS_MIN_VERSION"

	tests := []struct {
		name      string
		tlsConfig *tls.Config
		override  string
		wantMin   uint16
	}{
		{
			name: "existing config retains min version when no override configured",
			tlsConfig: &tls.Config{
				MinVersion: tls.VersionTLS10,
			},
			override: "",
			wantMin:  tls.VersionTLS10,
		},
		{
			name: "valid override sets min version to TLS 1.2",
			tlsConfig: &tls.Config{
				MinVersion: tls.VersionTLS10,
			},
			override: "TLS1.2",
			wantMin:  tls.VersionTLS12,
		},
		{
			name: "invalid override defaults to TLS 1.2",
			tlsConfig: &tls.Config{
				MinVersion: tls.VersionTLS10,
			},
			override: "TLS1.0",
			wantMin:  tls.VersionTLS12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := viper.GetString(tlsMinVersionKey)
			viper.Set(tlsMinVersionKey, tt.override)
			t.Cleanup(func() { viper.Set(tlsMinVersionKey, original) })

			ConfigureTLS(tt.tlsConfig)

			if tt.tlsConfig != nil {
				assert.Equal(t, tt.wantMin, tt.tlsConfig.MinVersion)
			}
		})
	}
}

func Test_buildRegistryTransport(t *testing.T) {
	tests := []struct {
		name      string
		tlsConfig *tls.Config
		wantNil   bool
	}{
		{
			name:      "returns non-nil transport with nil TLS config",
			tlsConfig: nil,
			wantNil:   false,
		},
		{
			name: "returns non-nil transport with TLS config",
			tlsConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRegistryTransport(tt.tlsConfig)
			if tt.wantNil {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)

			if tt.tlsConfig != nil {
				assert.NotNil(t, got.TLSClientConfig)
			}
		})
	}
}

func Test_buildRegistryClient(t *testing.T) {
	tests := []struct {
		name      string
		tlsConfig *tls.Config
		wantNil   bool
	}{
		{
			name:      "returns non-nil client with nil TLS config",
			tlsConfig: nil,
			wantNil:   false,
		},
		{
			name: "returns non-nil client with TLS config",
			tlsConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRegistryClient(tt.tlsConfig)
			if tt.wantNil {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.NotNil(t, got.Transport)
		})
	}
}

func TestNewAuthClient(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "returns non-nil client",
		},
		{
			name: "multiple calls return same client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAuthClient()
			assert.NotNil(t, got)

			if tt.name == "multiple calls return same client" {
				got2 := NewAuthClient()
				assert.Same(t, got, got2)
			}
		})
	}
}

// mockRoundTripper is a test helper that implements http.RoundTripper.
type mockRoundTripper struct {
	response *http.Response
	err      error
}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.response, nil
}

// mustNewRequest creates an HTTP GET request and fails the test on error.
func mustNewRequest(t *testing.T, url string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)

	return req
}
