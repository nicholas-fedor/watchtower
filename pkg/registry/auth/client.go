package auth

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Constants for HTTP client configuration.
// These values define timeouts and connection limits for registry requests.
const (
	DefaultTimeoutSeconds             = 30  // Default timeout for HTTP requests in seconds
	DefaultMaxIdleConns               = 100 // Maximum number of idle connections in the pool
	DefaultIdleConnTimeoutSeconds     = 90  // Timeout for idle connections in seconds
	DefaultTLSHandshakeTimeoutSeconds = 10  // Timeout for TLS handshake in seconds
	DefaultExpectContinueTimeout      = 1   // Timeout for expecting continue response in seconds
	DefaultDialTimeoutSeconds         = 30  // Timeout for establishing TCP connections in seconds
	DefaultDialKeepAliveSeconds       = 30  // Keep-alive probes for persistent connections in seconds
	DefaultMaxRedirects               = 3   // Maximum number of redirects to follow (reduced from Go's default of 10)
)

// TLSVersionMap maps string names to TLS version constants.
// It provides a lookup for configuring the minimum TLS version based on user settings.
// Only TLS 1.2 and TLS 1.3 are accepted as valid minimum versions.
// Older versions are rejected by the lookup and fall back to the default.
var TLSVersionMap = map[string]uint16{
	"TLS1.2": tls.VersionTLS12,
	"TLS1.3": tls.VersionTLS13,
}

// Cached client variables for HTTP client reuse.
var (
	cachedClient   Client    // Cached HTTP client for registry authentication requests.
	clientInitOnce sync.Once // Ensures the cached client is initialized only once.
)

// Client defines the interface for executing HTTP requests to container registries.
//
// This interface abstracts the HTTP client used for authentication operations, enabling
// dependency injection and facilitating unit testing with mock implementations.
type Client interface {
	// Do executes the provided HTTP request and returns the response or an error.
	//
	// Parameters:
	//   - req: The HTTP request to execute.
	//
	// Returns:
	//   - *http.Response: The HTTP response from the registry, if successful.
	//   - error: Non-nil if the request fails, nil otherwise.
	Do(req *http.Request) (*http.Response, error)
}

// registryClient is a concrete implementation of the Client interface.
//
// It encapsulates an HTTP client configured for registry interactions, providing a
// mechanism to execute authenticated requests with customizable TLS settings.
type registryClient struct {
	client *http.Client // The underlying HTTP client for making requests.
}

// Do executes an HTTP request using the underlying HTTP client.
//
// This method satisfies the Client interface, delegating the request execution
// to the embedded HTTP client.
//
// Parameters:
//   - request: The HTTP request to execute.
//
// Returns:
//   - *http.Response: The HTTP response from the registry, if successful.
//   - error: Non-nil if the request fails, nil otherwise.
func (r *registryClient) Do(request *http.Request) (*http.Response, error) {
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}

	return response, nil
}

// ConfigureTLS builds a TLS configuration from Viper settings.
//
// Parameters:
//   - tlsConfig: The base TLS configuration to modify.
func ConfigureTLS(tlsConfig *tls.Config) {
	// Configure TLS verification based on WATCHTOWER_REGISTRY_TLS_SKIP.
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		tlsConfig.InsecureSkipVerify = true

		logrus.Debug("TLS verification disabled via WATCHTOWER_REGISTRY_TLS_SKIP configuration")
	}

	// Configure minimum TLS version based on WATCHTOWER_REGISTRY_TLS_MIN_VERSION.
	minVersion := viper.GetString("WATCHTOWER_REGISTRY_TLS_MIN_VERSION")
	if minVersion != "" {
		version, ok := TLSVersionMap[strings.ToUpper(minVersion)]
		if ok {
			tlsConfig.MinVersion = version

			logrus.WithField("min_version", minVersion).
				Debug("Configured TLS minimum version")
		} else {
			tlsConfig.MinVersion = tls.VersionTLS12

			logrus.WithField("min_version", minVersion).
				Warn("Invalid TLS minimum version specified - defaulting to TLS 1.2")
		}
	}
}

// buildRegistryTransport creates an HTTP transport with the given TLS configuration.
//
// Parameters:
//   - tlsConfig: The TLS configuration to use.
//
// Returns:
//   - *http.Transport: Configured transport for registry requests.
func buildRegistryTransport(tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{
		TLSClientConfig:       tlsConfig,                                       // TLS configuration for secure registry connections.
		Proxy:                 http.ProxyFromEnvironment,                       // Respect proxy environment variables (e.g., HTTP_PROXY, HTTPS_PROXY).
		MaxIdleConns:          DefaultMaxIdleConns,                             // Maximum number of idle connections to keep open.
		IdleConnTimeout:       DefaultIdleConnTimeoutSeconds * time.Second,     // Timeout for closing idle connections.
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeoutSeconds * time.Second, // Timeout for completing TLS handshakes.
		ExpectContinueTimeout: DefaultExpectContinueTimeout * time.Second,      // Timeout for receiving HTTP 100-Continue responses.
		DialContext: (&net.Dialer{
			Timeout:   DefaultDialTimeoutSeconds * time.Second,   // Timeout for establishing TCP connections.
			KeepAlive: DefaultDialKeepAliveSeconds * time.Second, // Keep-alive probes for persistent connections.
		}).DialContext,
	}
}

// buildRegistryClient creates an HTTP client with the configured transport and timeouts.
//
// Parameters:
//   - tlsConfig: The TLS configuration to use.
//
// Returns:
//   - *http.Client: Configured HTTP client for registry requests.
func buildRegistryClient(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Transport: buildRegistryTransport(tlsConfig),
		Timeout:   DefaultTimeoutSeconds * time.Second, // Overall timeout for HTTP requests.
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= DefaultMaxRedirects { // Limit redirects to prevent excessive loops or attacks.
				return http.ErrUseLastResponse
			}

			return nil
		},
	}
}

// NewAuthClient returns a cached Client for registry authentication requests.
//
// The client is initialized once on the first call using Viper configuration
// values WATCHTOWER_REGISTRY_TLS_SKIP and WATCHTOWER_REGISTRY_TLS_MIN_VERSION.
// Subsequent calls return the same cached client instance. The client is configured
// with default timeouts and connection limits for registry access.
//
// Returns:
//   - Client: Ready for registry authentication requests.
func NewAuthClient() Client {
	clientInitOnce.Do(func() {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12, // Default to TLS 1.2 for secure communication.
		}

		ConfigureTLS(tlsConfig)
		cachedClient = &registryClient{
			client: buildRegistryClient(tlsConfig),
		}
	})

	return cachedClient
}
