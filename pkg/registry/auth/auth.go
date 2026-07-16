package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for auth operations.
var (
	errFailedCreateChallengeRequest  = errors.New("failed to create challenge request")
	errFailedExecuteChallengeRequest = errors.New("failed to execute challenge request")
	errNoCredentials                 = errors.New("no credentials available")
	errUnsupportedChallenge          = errors.New("unsupported challenge type from registry")
	errUnexpectedStatus              = errors.New("unexpected status with empty WWW-Authenticate header")
)

// TokenResult holds the result of a token acquisition operation.
type TokenResult struct {
	Token         string // Authentication token (e.g., "Basic ..." or "Bearer ...").
	ChallengeHost string // Challenge host (e.g., "ghcr.io"), empty if not applicable.
	Redirected    bool   // True if the challenge request was redirected, false otherwise.
	RedirectHost  string // The final host after redirects, empty if not redirected.
}

// challengeResponse holds parsed data from a challenge HTTP response.
type challengeResponse struct {
	statusCode    int    // HTTP status code from the challenge response.
	wwwAuthHeader string // WWW-Authenticate header value.
	redirected    bool   // Whether the request was redirected.
	redirectHost  string // Final host after redirects, empty if not redirected.
}

// resolveChallengeScheme determines the HTTP scheme for registry requests.
//
// Parameters:
//   - host: The registry host.
//
// Returns:
//   - string: The resolved scheme ("http" or "https").
func resolveChallengeScheme(host string) string {
	scheme := "https"
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		scheme = "http"

		logrus.WithField("host", host).
			Debug("Using HTTP scheme due to WATCHTOWER_REGISTRY_TLS_SKIP")
	}

	return scheme
}

// resolveEndpointHost extracts the host and scheme from an endpoint override.
//
// Parameters:
//   - endpoint: Optional registry host override, possibly with scheme.
//   - canonical: The canonical host to use when endpoint parsing fails.
//   - scheme: Default scheme to use when endpoint has no explicit scheme.
//
// Returns:
//   - string: The resolved host.
//   - string: The resolved scheme.
func resolveEndpointHost(endpoint, canonical, scheme string) (string, string) {
	if endpoint == "" {
		return canonical, "https"
	}

	endpointURL, err := url.Parse(endpoint)
	if err == nil && endpointURL.Host != "" {
		host := endpointURL.Host

		s := endpointURL.Scheme
		if s == "" {
			s = scheme
		}

		return host, s
	}

	// If parsing fails, use the endpoint as a bare host.
	return endpoint, scheme
}

// GetChallengeURL generates a challenge URL for accessing an image's registry.
//
// When endpoint is non-empty, it is used as the registry host for the challenge URL instead
// of the canonical registry host. The endpoint may include a scheme (e.g.,
// "https://mirror.example.com") or be a bare hostname.
//
// Parameters:
//   - imageRef: Normalized image reference.
//   - endpoint: Optional registry host override (e.g., mirror address). Empty string uses canonical host.
//
// Returns:
//   - url.URL: Generated challenge URL.
func GetChallengeURL(imageRef reference.Named, endpoint string) url.URL {
	host, _ := GetRegistryAddress(imageRef.Name())

	scheme := resolveChallengeScheme(host)

	endpointHost, endpointScheme := resolveEndpointHost(endpoint, host, scheme)
	if endpoint != "" {
		host = endpointHost
		scheme = endpointScheme
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
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url.String(),
		nil,
	)
	if err != nil {
		logrus.WithError(err).
			WithField("url", url.String()).
			Debug("Failed to create challenge request")

		return nil, fmt.Errorf("%w: %w", errFailedCreateChallengeRequest, err)
	}

	// Set headers for compatibility and identification.
	request.Header.Set("Accept", "*/*")
	request.Header.Set("User-Agent", meta.UserAgent)

	logrus.WithFields(logrus.Fields{
		"url": url.String(),
	}).Debug("Created challenge request")

	return request, nil
}

// handleSuccessfulChallenge returns a TokenResult indicating no authentication is required.
//
// Parameters:
//   - redirected: Whether the challenge request was redirected.
//   - redirectHost: The final host after redirects.
//
// Returns:
//   - TokenResult: Result with empty token and no challenge host.
func handleSuccessfulChallenge(redirected bool, redirectHost string) TokenResult {
	return TokenResult{
		Redirected:    redirected,
		RedirectHost:  redirectHost,
		ChallengeHost: "",
		Token:         "",
	}
}

// handleBasicAuthChallenge returns a TokenResult with Basic authentication.
//
// Parameters:
//   - registryAuth: Base64-encoded auth string.
//   - fields: Logging fields for context.
//   - redirected: Whether the challenge request was redirected.
//   - redirectHost: The final host after redirects.
//
// Returns:
//   - TokenResult: Result with Basic auth token.
//   - error: Non-nil if no credentials are provided, nil on success.
func handleBasicAuthChallenge(
	registryAuth string,
	fields logrus.Fields,
	redirected bool,
	redirectHost string,
) (TokenResult, error) {
	if registryAuth == "" {
		logrus.WithFields(fields).Debug("No credentials provided for Basic auth")

		return TokenResult{}, fmt.Errorf("%w: basic auth required", errNoCredentials)
	}

	logrus.WithFields(fields).Debug("Using Basic auth")

	return TokenResult{
		Token:         "Basic " + registryAuth,
		ChallengeHost: "",
		Redirected:    redirected,
		RedirectHost:  redirectHost,
	}, nil
}

// handleUnsupportedChallenge returns an error for unsupported challenge types.
//
// Parameters:
//   - challenge: The unsupported challenge type.
//   - fields: Logging fields for context.
//
// Returns:
//   - TokenResult: Empty result.
//   - error: Non-nil describing the unsupported challenge.
func handleUnsupportedChallenge(challenge string, fields logrus.Fields) (TokenResult, error) {
	logrus.WithFields(fields).
		WithField("challenge", challenge).
		Error("Unsupported challenge type from registry")

	return TokenResult{}, fmt.Errorf("%w: %s", errUnsupportedChallenge, challenge)
}

// processChallengeResponse interprets the challenge HTTP response and routes to the appropriate auth handler.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - container: Container with image info.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//   - redirected: Whether the challenge request was redirected.
//   - redirectHost: The final host after redirects.
//   - fields: Logging fields for context.
//   - response: Parsed challenge response data.
//
// Returns:
//   - TokenResult: Authentication result containing token and redirect info.
//   - error: Non-nil if operation fails, nil on success.
func processChallengeResponse(
	ctx context.Context,
	container types.Container,
	registryAuth string,
	client Client,
	redirected bool,
	redirectHost string,
	fields logrus.Fields,
	response challengeResponse,
) (TokenResult, error) {
	// Handle 200 OK response (no auth required).
	if response.statusCode == http.StatusOK {
		logrus.WithFields(fields).
			WithField("url", response.wwwAuthHeader).
			Debug("No authentication required (200 OK)")

		return handleSuccessfulChallenge(redirected, redirectHost), nil
	}

	// If the header is empty, return an authentication error for 401 responses.
	if response.wwwAuthHeader == "" {
		if response.statusCode == http.StatusUnauthorized {
			return TokenResult{}, fmt.Errorf("%w: no credentials available", errNoCredentials)
		}

		return TokenResult{}, fmt.Errorf(
			"%w: status %d",
			errUnexpectedStatus,
			response.statusCode,
		)
	}

	// Normalize challenge for comparison.
	challenge := strings.ToLower(strings.TrimSpace(response.wwwAuthHeader))
	logrus.WithFields(fields).WithField("challenge", challenge).
		Debug("Processing challenge type")

	// Handle Basic auth if specified.
	if strings.HasPrefix(challenge, "basic") {
		return handleBasicAuthChallenge(
			registryAuth,
			fields,
			redirected,
			redirectHost,
		)
	}

	// Handle Bearer auth.
	if strings.HasPrefix(challenge, "bearer") {
		return handleBearerAuth(
			ctx,
			response.wwwAuthHeader,
			container,
			registryAuth,
			client,
			redirected,
			redirectHost,
			fields,
		)
	}

	return handleUnsupportedChallenge(challenge, fields)
}

// handleBearerAuth handles the Bearer authentication challenge.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - wwwAuthHeader: The WWW-Authenticate header value.
//   - container: Container with image info.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//   - redirected: Whether the challenge request was redirected.
//   - redirectHost: The final host after redirects.
//   - fields: Logging fields for context.
//
// Returns:
//   - TokenResult: Authentication result containing token and redirect info.
//   - error: Non-nil if operation fails, nil on success.
func handleBearerAuth(
	ctx context.Context,
	wwwAuthHeader string,
	container types.Container,
	registryAuth string,
	client Client,
	redirected bool,
	redirectHost string,
	fields logrus.Fields,
) (TokenResult, error) {
	logrus.WithFields(fields).Debug("Entering Bearer auth path")

	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).WithFields(fields).
			Debug("Failed to parse image name")

		return TokenResult{}, fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	authURL, err := GetAuthURL(
		strings.ToLower(wwwAuthHeader),
		normalizedRef,
	)
	if err != nil {
		logrus.WithError(err).WithFields(fields).
			Debug("Failed to construct bearer auth URL")

		return TokenResult{}, fmt.Errorf("%w: %w", errFailedConstructBearerAuthURL, err)
	}

	challengeHost := authURL.Host
	if challengeHost != "" {
		logrus.WithFields(fields).
			WithField("challenge_host", challengeHost).
			Debug("Extracted challenge host")
	}

	token, err := GetBearerToken(
		ctx,
		wwwAuthHeader,
		normalizedRef,
		registryAuth,
		client,
	)
	if err != nil {
		logrus.WithError(err).WithFields(fields).
			Debug("Failed to get bearer token")

		return TokenResult{}, fmt.Errorf("%w: %w", errFailedDecodeResponse, err)
	}

	if token == "" {
		logrus.WithFields(fields).Debug("Empty bearer token received")

		return TokenResult{}, fmt.Errorf(
			"%w: empty token in response",
			errFailedDecodeResponse,
		)
	}

	logrus.WithFields(fields).
		WithField("token_present", token != "").
		WithField("challenge_host", challengeHost).
		Debug("Returning Bearer token and challenge host")

	return TokenResult{
		Token:         token,
		ChallengeHost: challengeHost,
		Redirected:    redirected,
		RedirectHost:  redirectHost,
	}, nil
}

// GetToken fetches a token and the challenge host for the registry hosting the provided image.
//
// When endpoint is non-empty, it is used as the registry host for the challenge URL
// instead of the canonical registry host. This enables digest checks against configured
// Docker registry mirrors.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - container: Container with image info.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//   - endpoint: Optional registry host override (e.g., mirror address). Empty string uses canonical host.
//
// Returns:
//   - TokenResult: Authentication result containing token, challenge host, and redirect info.
//   - error: Non-nil if operation fails, nil on success.
func GetToken(
	ctx context.Context,
	container types.Container,
	registryAuth string,
	client Client,
	endpoint string,
) (TokenResult, error) {
	fields := logrus.Fields{
		"image": container.ImageName(),
	}

	// Parse image name into a normalized reference.
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).WithFields(fields).
			Debug("Failed to parse image name")

		return TokenResult{}, fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	// Generate the challenge URL, using the endpoint override if provided.
	challengeURL := GetChallengeURL(normalizedRef, endpoint)
	logrus.WithFields(fields).
		WithField("url", challengeURL.String()).
		Debug("Constructed challenge URL")

	// Build and execute the challenge request.
	request, err := GetChallengeRequest(ctx, challengeURL)
	if err != nil {
		logrus.WithError(err).WithFields(fields).
			Debug("Failed to create challenge request")

		return TokenResult{}, fmt.Errorf("%w: %w", errFailedCreateChallengeRequest, err)
	}

	response, err := client.Do(request)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			WithField("url", challengeURL.String()).
			Debug("Failed to execute challenge request")

		return TokenResult{}, fmt.Errorf("%w: %w", errFailedExecuteChallengeRequest, err)
	}
	defer response.Body.Close()

	// Detect if the request was redirected by comparing the complete final URL.
	redirected := response.Request.URL.String() != challengeURL.String()

	// Capture the final host after redirects for use by callers.
	var redirectHost string
	if redirected {
		redirectHost = response.Request.URL.Host
		logrus.WithFields(fields).
			WithField("redirect_host", redirectHost).
			Debug("Challenge request was redirected to different URL")
	}

	// Extract the challenge header.
	wwwAuthHeader := response.Header.Get(ChallengeHeader)
	// Log endpoint in sanitized form (host only) to avoid leaking credentials.
	sanitizedEndpoint := endpoint
	if endpoint != "" {
		sanitizedEndpoint = "<redacted>"

		u, err := url.Parse(endpoint)
		if err == nil && u.Host != "" {
			sanitizedEndpoint = u.Host
		}
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"status":  response.Status,
		"header":  wwwAuthHeader,
		"mirrors": sanitizedEndpoint,
	}).Debug("Received challenge response")

	challengeResponse := challengeResponse{
		statusCode:    response.StatusCode,
		wwwAuthHeader: wwwAuthHeader,
		redirected:    redirected,
		redirectHost:  redirectHost,
	}

	// Route the challenge response to the appropriate auth handler.
	tokenResult, err := processChallengeResponse(
		ctx,
		container,
		registryAuth,
		client,
		redirected,
		redirectHost,
		fields,
		challengeResponse,
	)

	return tokenResult, err
}
