// Package auth provides functionality for authenticating with container registries.
// It handles token retrieval and challenge URL generation for registry access.
package auth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ChallengeHeader is the HTTP Header containing challenge instructions.
const ChallengeHeader = "WWW-Authenticate"

// Domains for Docker Hub, the default registry.
const (
	DockerRegistryDomain = "docker.io"
	DockerRegistryHost   = "index.docker.io"
	LSCRRegistry         = "lscr.io"
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

// Errors for authentication operations.
var (
	// errNoCredentials indicates no authentication credentials were provided when required.
	errNoCredentials = errors.New("no credentials available")
	// errUnsupportedChallenge indicates the registry returned an unrecognized authentication challenge type.
	errUnsupportedChallenge = errors.New("unsupported challenge type from registry")
	// errInvalidChallengeHeader indicates the challenge header lacks required fields for authentication.
	errInvalidChallengeHeader = errors.New(
		"challenge header did not include all values needed to construct an auth url",
	)
	// errInvalidRealmURL indicates the realm URL in the challenge header is malformed or invalid.
	errInvalidRealmURL = errors.New("invalid realm URL in challenge header")
	// errFailedCreateChallengeRequest indicates a failure to construct the HTTP request for the challenge.
	errFailedCreateChallengeRequest = errors.New("failed to create challenge request")
	// errFailedExecuteChallengeRequest indicates a failure to send or receive a response for the challenge request.
	errFailedExecuteChallengeRequest = errors.New("failed to execute challenge request")
	// errFailedCreateBearerRequest indicates a failure to construct the HTTP request for a bearer token.
	errFailedCreateBearerRequest = errors.New("failed to create bearer token request")
	// errFailedExecuteBearerRequest indicates a failure to send or receive a response for the bearer token request.
	errFailedExecuteBearerRequest = errors.New("failed to execute bearer token request")
	// errFailedUnmarshalBearerResponse indicates a failure to parse the bearer token response JSON.
	errFailedUnmarshalBearerResponse = errors.New("failed to unmarshal bearer token response")
	// errFailedParseImageName indicates a failure to parse the container image name into a normalized reference.
	errFailedParseImageName = errors.New("failed to parse image name")
	// errFailedDecodeResponse indicates a failure to decode the token response from the registry.
	errFailedDecodeResponse = errors.New("failed to decode response")
	// errFailedParseImageReference indicates a failure to parse an image reference into a normalized form.
	errFailedParseImageReference = errors.New("failed to parse image reference")
)

// TLSVersionMap maps string names to TLS version constants.
// It provides a lookup for configuring the minimum TLS version based on user settings.
var TLSVersionMap = map[string]uint16{
	"TLS1.0": tls.VersionTLS10,
	"TLS1.1": tls.VersionTLS11,
	"TLS1.2": tls.VersionTLS12,
	"TLS1.3": tls.VersionTLS13,
}

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
//   - req: The HTTP request to execute.
//
// Returns:
//   - *http.Response: The HTTP response from the registry, if successful.
//   - error: Non-nil if the request fails, nil otherwise.
func (r *registryClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}

	return resp, nil
}

// NewAuthClient creates a new Client instance configured with TLS settings.
//
// It initializes the HTTP client based on Viper configuration values for
// WATCHTOWER_REGISTRY_TLS_SKIP and WATCHTOWER_REGISTRY_TLS_MIN_VERSION, setting
// appropriate TLS verification and minimum version requirements. The client is
// configured with default timeouts and connection limits for robust registry access.
//
// Returns:
//   - Client: A new Client instance ready for registry authentication requests.
func NewAuthClient() Client {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12, // Default to TLS 1.2 for secure communication.
	}

	// Configure TLS verification based on WATCHTOWER_REGISTRY_TLS_SKIP.
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		tlsConfig.InsecureSkipVerify = true

		logrus.Debug("TLS verification disabled via WATCHTOWER_REGISTRY_TLS_SKIP configuration")
	}

	// Configure minimum TLS version based on WATCHTOWER_REGISTRY_TLS_MIN_VERSION.
	if minVersion := viper.GetString("WATCHTOWER_REGISTRY_TLS_MIN_VERSION"); minVersion != "" {
		if version, ok := TLSVersionMap[strings.ToUpper(minVersion)]; ok {
			tlsConfig.MinVersion = version

			logrus.WithField("min_version", minVersion).Debug("Configured TLS minimum version")
		} else {
			logrus.WithField("min_version", minVersion).Warn("Invalid TLS minimum version specified; defaulting to TLS 1.2")
		}
	}

	return &registryClient{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,                 // TLS configuration for secure registry connections.
				Proxy:           http.ProxyFromEnvironment, // Respect proxy environment variables (e.g., HTTP_PROXY, HTTPS_PROXY).
				DialContext: (&net.Dialer{
					Timeout:   DefaultDialTimeoutSeconds * time.Second,   // Timeout for establishing TCP connections.
					KeepAlive: DefaultDialKeepAliveSeconds * time.Second, // Keep-alive probes for persistent connections.
				}).DialContext,
				MaxIdleConns:          DefaultMaxIdleConns,                             // Maximum number of idle connections to keep open.
				IdleConnTimeout:       DefaultIdleConnTimeoutSeconds * time.Second,     // Timeout for closing idle connections.
				TLSHandshakeTimeout:   DefaultTLSHandshakeTimeoutSeconds * time.Second, // Timeout for completing TLS handshakes.
				ExpectContinueTimeout: DefaultExpectContinueTimeout * time.Second,      // Timeout for receiving HTTP 100-Continue responses.
			},
			Timeout: DefaultTimeoutSeconds * time.Second, // Overall timeout for HTTP requests.
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(
					via,
				) >= DefaultMaxRedirects { // Limit redirects to prevent excessive loops or attacks.
					return http.ErrUseLastResponse
				}

				return nil
			},
		},
	}
}

// extractChallengeHost extracts the host from a realm URL (e.g., "https://ghcr.io/token" -> "ghcr.io").
//
// Parameters:
//   - realm: The realm URL from the WWW-Authenticate header.
//   - fields: Logging fields for context.
//
// Returns:
//   - string: The extracted host, or empty if extraction fails.
func extractChallengeHost(realm string, fields logrus.Fields) string {
	realm = strings.TrimSpace(realm)
	logrus.WithFields(fields).
		WithField("trimmed_realm", realm).
		Debug("Trimmed realm for host extraction")

	for _, prefix := range []string{"https://", "http://"} {
		if after, ok := strings.CutPrefix(realm, prefix); ok {
			realm = after
			if idx := strings.Index(realm, "/"); idx != -1 {
				return realm[:idx]
			}

			return realm
		}
	}

	logrus.WithFields(fields).
		WithField("realm", realm).
		Debug("Failed to extract challenge host from realm")

	return ""
}

// handleBearerAuth processes Bearer authentication for a container registry.
//
// It parses the challenge header, extracts the challenge host, and fetches a bearer token.
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - wwwAuthHeader: The WWW-Authenticate header value.
//   - container: Container with image info.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//   - redirected: True if the challenge request was redirected.
//   - fields: Logging fields for context.
//
// Returns:
//   - string: Bearer token header (e.g., "Bearer ...").
//   - string: Challenge host (e.g., "ghcr.io").
//   - bool: Redirect flag (passed through).
//   - error: Non-nil if processing fails, nil on success.
func handleBearerAuth(
	ctx context.Context,
	wwwAuthHeader string,
	container types.Container,
	registryAuth string,
	client Client,
	redirected bool,
	fields logrus.Fields,
) (string, string, bool, error) {
	logrus.WithFields(fields).Debug("Entering Bearer auth path")

	var challengeHost string

	// Parse the WWW-Authenticate header.
	scope, realm, service, err := processChallenge(wwwAuthHeader, container.ImageName())
	logrus.WithFields(fields).
		WithField("realm", realm).
		WithField("service", service).
		WithField("scope", scope).
		WithField("err", err).
		Debug("Processed challenge header")

	switch {
	case err != nil:
		logrus.WithError(err).WithFields(fields).Debug("Failed to process challenge header")
		// Proceed with token retrieval, as challengeHost is optional.
	case realm != "":
		challengeHost = extractChallengeHost(realm, fields)
		if challengeHost != "" {
			logrus.WithFields(fields).
				WithField("challenge_host", challengeHost).
				Debug("Extracted challenge host")
		}
	default:
		logrus.WithFields(fields).Debug("Empty realm in challenge header")
	}

	// Fetch the bearer token.
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to parse image name")

		return "", "", redirected, fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	token, err := GetBearerHeader(
		ctx,
		strings.ToLower(wwwAuthHeader),
		normalizedRef,
		registryAuth,
		client,
	)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get bearer token")

		return "", "", redirected, fmt.Errorf("%w: %w", errFailedDecodeResponse, err)
	}

	if token == "" {
		logrus.WithFields(fields).Debug("Empty bearer token received")

		return "", "", redirected, fmt.Errorf(
			"%w: empty token in response",
			errFailedDecodeResponse,
		)
	}

	logrus.WithFields(fields).
		WithField("token_present", token != "").
		WithField("challenge_host", challengeHost).
		Debug("Returning Bearer token and challenge host")

	return token, challengeHost, redirected, nil
}

// GetToken fetches a token and the challenge host for the registry hosting the provided image.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - container: Container with image info.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//
// Returns:
//   - string: Authentication token (e.g., "Basic ..." or "Bearer ...").
//   - string: Challenge host (e.g., "ghcr.io"), empty if not applicable.
//   - bool: True if the challenge request was redirected, false otherwise.
//   - error: Non-nil if operation fails, nil on success.
func GetToken(
	ctx context.Context,
	container types.Container,
	registryAuth string,
	client Client,
) (string, string, bool, error) {
	fields := logrus.Fields{
		"image": container.ImageName(),
	}

	// Parse image name into a normalized reference.
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to parse image name")

		return "", "", false, fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	// Generate the challenge URL.
	challengeURL := GetChallengeURL(normalizedRef)
	logrus.WithFields(fields).
		WithField("url", challengeURL.String()).
		Debug("Constructed challenge URL")

	// Build and execute the challenge request.
	req, err := GetChallengeRequest(ctx, challengeURL)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to create challenge request")

		return "", "", false, fmt.Errorf("%w: %w", errFailedCreateChallengeRequest, err)
	}

	res, err := client.Do(req)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			WithField("url", challengeURL.String()).
			Debug("Failed to execute challenge request")

		return "", "", false, fmt.Errorf("%w: %w", errFailedExecuteChallengeRequest, err)
	}
	defer res.Body.Close()

	// Detect if the request was redirected.
	redirected := res.Request.URL.Host != challengeURL.Host

	// Handle 200 OK response (no auth required).
	if res.StatusCode == http.StatusOK {
		logrus.WithFields(fields).
			WithField("url", challengeURL.String()).
			Debug("No authentication required (200 OK)")

		return "", "", redirected, nil
	}

	// Extract the challenge header.
	wwwAuthHeader := res.Header.Get(ChallengeHeader)
	logrus.WithFields(fields).WithFields(logrus.Fields{
		"status": res.Status,
		"header": wwwAuthHeader,
	}).Debug("Received challenge response")

	// If the header is empty, assume no authentication is required.
	if wwwAuthHeader == "" {
		logrus.WithFields(fields).
			WithField("url", challengeURL.String()).
			Debug("Empty WWW-Authenticate header; assuming no authentication required")

		return "", "", redirected, nil
	}

	// Normalize challenge for comparison.
	challenge := strings.ToLower(strings.TrimSpace(wwwAuthHeader))
	logrus.WithFields(fields).WithField("challenge", challenge).Debug("Processing challenge type")

	// Handle Basic auth if specified.
	if strings.HasPrefix(challenge, "basic") {
		if registryAuth == "" {
			logrus.WithFields(fields).Debug("No credentials provided for Basic auth")

			return "", "", redirected, fmt.Errorf("%w: basic auth required", errNoCredentials)
		}

		logrus.WithFields(fields).Debug("Using Basic auth")

		return "Basic " + registryAuth, "", redirected, nil
	}

	// Handle Bearer auth.
	if strings.HasPrefix(challenge, "bearer") {
		return handleBearerAuth(
			ctx,
			wwwAuthHeader,
			container,
			registryAuth,
			client,
			redirected,
			fields,
		)
	}

	// Handle unknown challenge types.
	logrus.WithFields(fields).
		WithField("challenge", challenge).
		Error("Unsupported challenge type from registry")

	return "", "", redirected, fmt.Errorf("%w: %s", errUnsupportedChallenge, challenge)
}

// processChallenge parses the WWW-Authenticate header to extract authentication details.
//
// It supports Bearer authentication, extracting the realm, service, and optional scope for token requests.
//
// Parameters:
//   - wwwAuthHeader: The WWW-Authenticate header value (e.g., 'Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:linuxserver/nginx:pull"').
//   - image: The image name for logging context.
//
// Returns:
//   - string: The scope for the token request (e.g., "repository:linuxserver/nginx:pull"), or empty if not provided.
//   - string: The realm URL for the token request (e.g., "https://ghcr.io/token").
//   - string: The service identifier (e.g., "ghcr.io").
//   - error: Non-nil if parsing fails critically (missing realm or service), nil otherwise.
func processChallenge(wwwAuthHeader, image string) (string, string, string, error) {
	fields := logrus.Fields{
		"image":     image,
		"challenge": wwwAuthHeader,
	}
	logrus.WithFields(fields).Debug("Processing challenge type")

	if !strings.HasPrefix(strings.ToLower(wwwAuthHeader), "bearer") {
		logrus.WithFields(fields).Debug("Unsupported challenge type")

		return "", "", "", fmt.Errorf("%w: %s", errUnsupportedChallenge, wwwAuthHeader)
	}

	// Split header into key-value pairs (e.g., realm="...",service="...").
	parts := strings.Split(strings.TrimPrefix(strings.ToLower(wwwAuthHeader), "bearer "), ",")
	values := make(map[string]string)

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if key, val, ok := strings.Cut(trimmed, "="); ok {
			// Remove quotes from value if present.
			values[key] = strings.Trim(val, `"`)
		}
	}

	realm, realmOK := values["realm"]
	service, serviceOK := values["service"]
	scope := values["scope"] // Scope is optional

	if !realmOK || !serviceOK {
		logrus.WithFields(fields).Warn("Missing required challenge header values: realm or service")

		return "", "", "", fmt.Errorf(
			"%w: missing realm or service in header: %s",
			errInvalidChallengeHeader,
			wwwAuthHeader,
		)
	}

	if scope == "" {
		logrus.WithFields(fields).
			Debug("Scope missing in WWW-Authenticate header; will be constructed dynamically")
	} else {
		logrus.WithFields(fields).WithField("scope", scope).Debug("Set auth token scope")
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"realm":   realm,
		"service": service,
		"scope":   scope,
	}).Debug("Parsed challenge header")

	return scope, realm, service, nil
}

// GetChallengeRequest creates a request for retrieving challenge instructions.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - url: URL for the challenge request.
//
// Returns:
//   - *http.Request: Constructed request if successful.
//   - error: Non-nil if creation fails, nil on success.
func GetChallengeRequest(ctx context.Context, url url.URL) (*http.Request, error) {
	// Create a GET request with context for cancellation and timeout.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		logrus.WithError(err).
			WithField("url", url.String()).
			Debug("Failed to create challenge request")

		return nil, fmt.Errorf("%w: %w", errFailedCreateChallengeRequest, err)
	}

	// Set headers for compatibility and identification.
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Watchtower (Docker)")

	logrus.WithFields(logrus.Fields{
		"url": url.String(),
	}).Debug("Created challenge request")

	return req, nil
}

// GetBearerHeader fetches a bearer token based on challenge instructions.
//
// Parameters:
//   - ctx: Context for request lifecycle control, enabling cancellation or timeouts.
//   - challenge: Challenge string from the registry’s WWW-Authenticate header.
//   - imageRef: Normalized image reference for scoping the token request.
//   - registryAuth: Base64-encoded auth string (e.g., "username:password").
//   - client: Client instance for executing HTTP requests.
//
// Returns:
//   - string: Bearer token header (e.g., "Bearer ...") if successful.
//   - error: Non-nil if the operation fails, nil on success.
func GetBearerHeader(
	ctx context.Context,
	challenge string,
	imageRef reference.Named,
	registryAuth string,
	client Client,
) (string, error) {
	// Construct the auth URL from the challenge details.
	authURL, err := GetAuthURL(challenge, imageRef)
	if err != nil {
		return "", err
	}

	// Build the token request with context.
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL.String(), nil)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image": imageRef.Name(),
			"url":   authURL.String(),
		}).Debug("Failed to create bearer token request")

		return "", fmt.Errorf("%w: %w", errFailedCreateBearerRequest, err)
	}

	// Add Basic auth header if credentials are provided.
	if registryAuth != "" {
		logrus.WithFields(logrus.Fields{
			"image": imageRef.Name(),
		}).Debug("Found credentials")

		if logrus.GetLevel() == logrus.TraceLevel {
			logrus.WithFields(logrus.Fields{
				"image":        imageRef.Name(),
				"registryAuth": registryAuth,
			}).Trace("Using credentials")
		}

		r.Header.Add("Authorization", "Basic "+registryAuth)
	} else {
		logrus.WithFields(logrus.Fields{
			"image": imageRef.Name(),
		}).Debug("No credentials found")
	}

	// Execute the token request.
	logrus.WithField("url", r.URL.String()).Debug("Sending bearer token request")

	authResponse, err := client.Do(r)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image": imageRef.Name(),
			"url":   authURL.String(),
		}).Debug("Failed to execute bearer token request")

		return "", fmt.Errorf("%w: %w", errFailedExecuteBearerRequest, err)
	}

	defer authResponse.Body.Close()

	// Read and parse the response body into a token structure.
	body, _ := io.ReadAll(authResponse.Body)
	tokenResponse := &types.TokenResponse{}

	err = json.Unmarshal(body, tokenResponse)
	if err != nil {
		logrus.WithError(err).
			WithField("image", imageRef.Name()).
			Debug("Failed to unmarshal bearer token response")

		return "", fmt.Errorf("%w: %w", errFailedUnmarshalBearerResponse, err)
	}

	logrus.WithFields(logrus.Fields{
		"image": imageRef.Name(),
	}).Debug("Retrieved bearer token")

	return "Bearer " + tokenResponse.Token, nil
}

// GetAuthURL constructs an authentication URL from challenge instructions.
//
// Parameters:
//   - challenge: Challenge string from the registry.
//   - imageRef: Normalized image reference.
//
// Returns:
//   - *url.URL: Constructed auth URL if successful.
//   - error: Non-nil if parsing fails, nil on success.
func GetAuthURL(challenge string, imageRef reference.Named) (*url.URL, error) {
	// Normalize and trim the challenge string for parsing.
	loweredChallenge := strings.ToLower(challenge)
	raw := strings.TrimPrefix(loweredChallenge, "bearer")

	// Parse key-value pairs from the challenge.
	pairs := strings.Split(raw, ",")
	values := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		trimmed := strings.Trim(pair, " ")
		if key, val, ok := strings.Cut(trimmed, "="); ok {
			values[key] = strings.Trim(val, `"`)
		}
	}

	// Get registry address for special handling.
	registryAddress, err := GetRegistryAddress(imageRef.Name())
	if err != nil {
		logrus.WithError(err).
			WithField("image", imageRef.Name()).
			Debug("Failed to get registry address")
	}

	// Special handling for lscr.io registry authentication:
	// lscr.io (LinuxServer.io) images are hosted on GitHub Container Registry (ghcr.io),
	// but the authentication challenge from lscr.io redirects to ghcr.io. When we receive
	// the WWW-Authenticate header from ghcr.io, it contains the ghcr.io realm URL.
	// However, for lscr.io images, we need to ensure the realm points to ghcr.io's token endpoint
	// to get the correct authentication tokens. This prevents authentication failures
	// that would occur if we tried to use lscr.io's non-existent token endpoint.
	//
	// Without this override, lscr.io authentication would fail because:
	// 1. Challenge request to lscr.io/v2/ redirects to ghcr.io/v2/
	// 2. ghcr.io returns WWW-Authenticate header with ghcr.io realm
	// 3. But sometimes the realm might not be correctly set, causing token requests to fail
	// 4. This ensures we always use the correct ghcr.io token endpoint for lscr.io images
	if registryAddress == LSCRRegistry {
		values["realm"] = "https://ghcr.io/token"
	}

	logrus.WithFields(logrus.Fields{
		"image":   imageRef.Name(),
		"realm":   values["realm"],
		"service": values["service"],
		"scope":   values["scope"],
	}).Debug("Parsed challenge header")

	// Validate required fields.
	if values["realm"] == "" || values["service"] == "" {
		return nil, errInvalidChallengeHeader
	}

	// Parse the realm into a URL.
	authURL, err := url.Parse(values["realm"])
	if err != nil || authURL == nil {
		clog := logrus.WithFields(logrus.Fields{
			"image": imageRef.Name(),
			"realm": values["realm"],
		})
		if err != nil {
			clog.WithError(err).Debug("Failed to parse realm URL")
		} else {
			clog.Debug("Invalid realm URL (nil after parsing)")
		}

		return nil, fmt.Errorf("%w: %s", errInvalidRealmURL, values["realm"])
	}

	// Add query parameters for service and scope.
	query := authURL.Query()
	query.Add("service", values["service"])

	scopeImage := reference.Path(imageRef)
	scope := fmt.Sprintf("repository:%s:pull", scopeImage)
	logrus.WithFields(logrus.Fields{
		"image": imageRef.Name(),
		"scope": scope,
	}).Debug("Set auth token scope")

	query.Add("scope", scope)
	authURL.RawQuery = query.Encode()

	logrus.WithFields(logrus.Fields{
		"image": imageRef.Name(),
		"url":   authURL.String(),
	}).Debug("Constructed auth URL")

	return authURL, nil
}

// GetRegistryAddress extracts the registry address from an image reference.
//
// It returns the domain part of the reference, mapping Docker Hub’s default domain to its canonical host if needed.
//
// Parameters:
//   - imageRef: Image reference string (e.g., "docker.io/library/alpine").
//
// Returns:
//   - string: Registry address (e.g., "index.docker.io") if successful.
//   - error: Non-nil if parsing fails, nil on success.
func GetRegistryAddress(imageRef string) (string, error) {
	// Parse the image reference into a normalized form for consistent domain extraction.
	normalizedRef, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		logrus.WithError(err).
			WithField("image_ref", imageRef).
			Debug("Failed to parse image reference")

		return "", fmt.Errorf("%w: %w", errFailedParseImageReference, err)
	}

	// Extract the domain from the normalized reference.
	address := reference.Domain(normalizedRef)

	// Map Docker Hub’s default domain to its canonical host for registry requests.
	if address == DockerRegistryDomain {
		logrus.WithFields(logrus.Fields{
			"image_ref": imageRef,
			"address":   address,
		}).Debug("Mapped Docker Hub domain to canonical host")

		address = DockerRegistryHost
	}

	logrus.WithFields(logrus.Fields{
		"image_ref": imageRef,
		"address":   address,
	}).Debug("Extracted registry address")

	return address, nil
}

// TransformAuth converts a base64-encoded JSON object into a base64-encoded "username:password" string.
// It decodes the input, extracts username and password from a RegistryCredentials struct, and re-encodes
// them for use in HTTP Basic Authentication headers, ensuring compatibility with registry requirements.
//
// Parameters:
//   - registryAuth: A base64-encoded string, typically a JSON object with username and password fields.
//
// Returns:
//   - string: A base64-encoded "username:password" string if credentials are present, otherwise the original input.
func TransformAuth(registryAuth string) string {
	// Decode the base64 input, silently ignoring errors to handle malformed inputs gracefully.
	b, _ := base64.StdEncoding.DecodeString(registryAuth)
	credentials := &types.RegistryCredentials{}

	// Unmarshal JSON into credentials struct, ignoring errors if malformed.
	_ = json.Unmarshal(b, credentials)

	// If both username and password are present, re-encode them as "username:password".
	if credentials.Username != "" && credentials.Password != "" {
		ba := fmt.Appendf(nil, "%s:%s", credentials.Username, credentials.Password)
		registryAuth = base64.StdEncoding.EncodeToString(ba)
	}

	return registryAuth // Return original if no valid credentials.
}

// GetChallengeURL generates a challenge URL for accessing an image's registry.
//
// Parameters:
//   - imageRef: Normalized image reference.
//
// Returns:
//   - url.URL: Generated challenge URL.
func GetChallengeURL(imageRef reference.Named) url.URL {
	// Extract registry host from the image reference.
	host, _ := GetRegistryAddress(imageRef.Name())

	// Special handling for lscr.io registry: use ghcr.io for challenge URL
	// to get the correct WWW-Authenticate header with proper scope.
	if host == "lscr.io" {
		host = "ghcr.io"
	}

	scheme := "https"
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		scheme = "http"

		logrus.WithField("host", host).
			Debug("Using HTTP scheme due to WATCHTOWER_REGISTRY_TLS_SKIP")
	}

	URL := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/v2/",
	}
	logrus.WithFields(logrus.Fields{
		"image": imageRef.Name(),
		"url":   URL.String(),
	}).Debug("Generated challenge URL")

	return URL
}
