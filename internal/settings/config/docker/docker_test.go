package docker

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/testutil/httpstub"
)

// TestDocker_ConnectionOptions checks that a client built from the options
// pings the daemon over TLS with a certificate path and verifies the daemon's
// certificate only when TLS verification is on.
func TestDocker_ConnectionOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// certFiles adjusts the certificate files the stub server wrote.
		certFiles func(t *testing.T, certPath string)
		tlsVerify bool
		wantPing  bool
	}{
		{
			name:      "verified TLS",
			certFiles: func(*testing.T, string) {},
			tlsVerify: true,
			wantPing:  true,
		},
		{
			name: "unverified TLS without a CA file",
			certFiles: func(t *testing.T, certPath string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(certPath, "ca.pem")))
			},
			tlsVerify: false,
			wantPing:  true,
		},
		{
			name: "verification rejects an untrusted daemon",
			certFiles: func(t *testing.T, certPath string) {
				t.Helper()
				// The self-signed client certificate did not sign the server's.
				clientCert, err := os.ReadFile(filepath.Join(certPath, "cert.pem"))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(certPath, "ca.pem"), clientCert, 0o600))
			},
			tlsVerify: true,
			wantPing:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A rejected certificate is logged by the server, so its log is
			// discarded.
			server := httpstub.NewUnstarted(t)
			server.TestServer().Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TestServer().StartTLS()
			server.RouteToHandler(http.MethodHead, httpstub.Path("/_ping"),
				httpstub.RespondWith(http.StatusOK, "", http.Header{"Api-Version": []string{"1.45"}}))

			certPath := server.WriteDockerCerts(t)
			tt.certFiles(t, certPath)

			options, err := Docker{
				Host:       "tcp://" + server.Addr(),
				APIVersion: "",
				CertPath:   certPath,
				TLSVerify:  tt.tlsVerify,
			}.ConnectionOptions()
			require.NoError(t, err)

			client, err := dockerClient.New(options...)
			require.NoError(t, err)

			_, err = client.Ping(t.Context(), dockerClient.PingOptions{})
			if !tt.wantPing {
				require.Error(t, err)
				assert.Empty(t, server.ReceivedRequests())

				return
			}

			require.NoError(t, err)

			requests := server.ReceivedRequests()
			require.NotEmpty(t, requests)
			assert.NotNil(t, requests[0].TLS, "the ping is sent over TLS")
		})
	}
}

// TestDocker_ConnectionOptions_plain checks that settings without a
// certificate path connect without TLS, and that an empty host keeps the
// Docker SDK's default socket.
func TestDocker_ConnectionOptions_plain(t *testing.T) {
	t.Parallel()

	server := httpstub.New(t)
	server.RouteToHandler(http.MethodHead, httpstub.Path("/_ping"),
		httpstub.RespondWith(http.StatusOK, "", http.Header{"Api-Version": []string{"1.45"}}))

	options, err := Docker{Host: "tcp://" + server.Addr(), APIVersion: "", CertPath: "", TLSVerify: false}.ConnectionOptions()
	require.NoError(t, err)

	client, err := dockerClient.New(options...)
	require.NoError(t, err)

	_, err = client.Ping(t.Context(), dockerClient.PingOptions{})
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(server.ReceivedRequests(), func(r *http.Request) bool { return r.TLS == nil }))

	options, err = Docker{}.ConnectionOptions()
	require.NoError(t, err)

	client, err = dockerClient.New(options...)
	require.NoError(t, err)
	assert.Equal(t, dockerClient.DefaultDockerHost, client.DaemonHost())
}

// TestDocker_ConnectionOptions_missingCertificates checks that a certificate
// path without its files returns an error naming the path.
func TestDocker_ConnectionOptions_missingCertificates(t *testing.T) {
	t.Parallel()

	certPath := t.TempDir()

	options, err := Docker{Host: "tcp://docker.example.com:2376", APIVersion: "", CertPath: certPath, TLSVerify: true}.ConnectionOptions()
	require.ErrorContains(t, err, fmt.Sprintf("configure TLS from %q", certPath))
	assert.Nil(t, options)
}
