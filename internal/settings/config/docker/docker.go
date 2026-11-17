package docker

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/docker/go-connections/tlsconfig"

	dockerClient "github.com/moby/moby/client"
)

// Docker holds Docker daemon connection settings.
type Docker struct {
	// Host is the Docker daemon socket or host URL.
	Host string
	// APIVersion is the Docker API version, or empty for negotiation.
	APIVersion string
	// CertPath is the path to TLS certificates.
	CertPath string
	// TLSVerify enables TLS verification for remote daemons.
	TLSVerify bool
}

// idleConnTimeout is how long an idle connection to the daemon stays open.
//
// It matches the Docker SDK's default transport, which closes idle connections
// so that a long-running process does not keep them.
const idleConnTimeout = 30 * time.Second

// ConnectionOptions returns the Docker SDK options that connect to the daemon these
// settings name, without an API version.
//
// A certificate path gives the connection TLS with the client certificate in cert.pem and
// key.pem, and TLS 1.2 as the lowest version. With TLS verification on, the daemon's
// certificate must be signed by the CA in ca.pem or by a system root. With it off, the
// daemon's certificate is not checked and ca.pem is not read. An empty host keeps the
// SDK's default socket.
//
// Returns:
//   - []dockerClient.Opt: Options for the TLS transport and the host.
//   - error: Non-nil when the certificate files cannot be loaded.
func (d Docker) ConnectionOptions() ([]dockerClient.Opt, error) {
	var options []dockerClient.Opt

	// These match the SDK's own environment options, with the idle timeout of its default
	// transport. The TLS option replaces the HTTP client, so it comes first, and the host
	// option then configures the new transport.
	if d.CertPath != "" {
		// tlsconfig adds ca.pem to the system roots and skips it when verification is off.
		tlsConfig, err := tlsconfig.Client(tlsconfig.Options{
			CAFile:             filepath.Join(d.CertPath, "ca.pem"),
			CertFile:           filepath.Join(d.CertPath, "cert.pem"),
			KeyFile:            filepath.Join(d.CertPath, "key.pem"),
			InsecureSkipVerify: !d.TLSVerify,
			MinVersion:         tls.VersionTLS12,
		})
		if err != nil {
			return nil, fmt.Errorf("configure TLS from %q: %w", d.CertPath, err)
		}

		options = append(options, dockerClient.WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
				IdleConnTimeout: idleConnTimeout,
			},
			CheckRedirect: dockerClient.CheckRedirect,
		}))
	}

	if d.Host != "" {
		options = append(options, dockerClient.WithHost(d.Host))
	}

	return options, nil
}
