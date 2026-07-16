package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/distribution/reference"
	"github.com/maypok86/otter/v2"
	"github.com/maypok86/otter/v2/stats"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for authentication operations.
var (
	errFailedCreateBearerRequest     = errors.New("failed to create bearer token request")
	errFailedConstructBearerAuthURL  = errors.New("failed to construct bearer auth url")
	errFailedExecuteBearerRequest    = errors.New("failed to execute bearer token request")
	errFailedUnmarshalBearerResponse = errors.New("failed to unmarshal bearer token response")
	errFailedDecodeResponse          = errors.New("failed to decode response")
	errFailedParseImageName          = errors.New("failed to parse image name")
	errInvalidRealmURL               = errors.New("invalid realm URL in challenge header")
)

// Default token TTL when the registry does not provide expires_in.
const defaultTokenTTL = 60 * time.Second

// Cache configuration constants.
const (
	tokenCacheMaximumSize     = 100_000
	tokenCacheInitialCapacity = 16
)

// Package-level token cache variables.
var (
	tokenCache     *otter.Cache[string, tokenCacheEntry]
	tokenCacheOnce sync.Once
)

// tokenCacheEntry stores a bearer token and its absolute expiry time.
type tokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

// tokenExpiryCalculator computes per-entry TTL based on the token's expires_in field.
type tokenExpiryCalculator struct{}

// ExpireAfterCreate returns the duration until the token expires.
func (e *tokenExpiryCalculator) ExpireAfterCreate(entry otter.Entry[string, tokenCacheEntry]) time.Duration {
	return time.Until(entry.Value.expiresAt)
}

// ExpireAfterUpdate returns the duration until the updated token expires.
func (e *tokenExpiryCalculator) ExpireAfterUpdate(entry otter.Entry[string, tokenCacheEntry], _ tokenCacheEntry) time.Duration {
	return time.Until(entry.Value.expiresAt)
}

// ExpireAfterRead preserves the existing expiry on reads.
func (e *tokenExpiryCalculator) ExpireAfterRead(entry otter.Entry[string, tokenCacheEntry]) time.Duration {
	return entry.ExpiresAfter()
}

// initTokenCache initializes the bearer token cache with Otter.
func initTokenCache() {
	tokenCacheOnce.Do(func() {
		counter := stats.NewCounter()

		cache, err := otter.New(
			&otter.Options[string, tokenCacheEntry]{
				MaximumSize:      tokenCacheMaximumSize,
				InitialCapacity:  tokenCacheInitialCapacity,
				ExpiryCalculator: &tokenExpiryCalculator{},
				StatsRecorder:    counter,
			})
		if err != nil {
			panic(fmt.Sprintf("failed to initialize token cache: %v", err))
		}

		tokenCache = cache

		logrus.Debug("Initialized bearer token cache")
	})
}

// GetBearerToken fetches a bearer token based on challenge instructions.
//
// It parses the challenge header, constructs the auth URL, and retrieves a bearer token.
// The token is cached to avoid redundant HTTP requests for the same challenge.
//
// Parameters:
//   - ctx: Context for request lifecycle control, enabling cancellation or timeouts.
//   - challenge: Challenge string from the registry's WWW-Authenticate header.
//   - imageRef: Normalized image reference for scoping the token request.
//   - registryAuth: Base64-encoded auth string (e.g., "username:password").
//   - client: Client instance for executing HTTP requests.
//
// Returns:
//   - string: Bearer token header (e.g., "Bearer ...") if successful.
//   - error: Non-nil if the operation fails, nil on success.
func GetBearerToken(
	ctx context.Context,
	challenge string,
	imageRef reference.Named,
	registryAuth string,
	client Client,
) (string, error) {
	fields := logrus.Fields{
		"image": imageRef.Name(),
	}
	logrus.WithFields(fields).Debug("Fetching bearer token from challenge")

	// Construct the auth URL from the challenge details.
	authURL, err := GetAuthURL(challenge, imageRef)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFailedConstructBearerAuthURL, err)
	}

	token, err := executeBearerTokenRequest(
		ctx,
		authURL,
		imageRef.Name(),
		registryAuth,
		client,
	)
	if err != nil {
		return "", err
	}

	logrus.WithFields(fields).
		WithField("token_present", token != "").
		Debug("Fetched bearer token")

	return token, nil
}

// executeBearerTokenRequest retrieves a bearer token, using the cache when possible.
//
// It checks the token cache for a valid, unexpired entry. On a cache miss or expiry,
// it executes the HTTP request, parses the response, and populates the cache.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - authURL: Pre-parsed authentication URL.
//   - imageName: Image name for logging context.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//
// Returns:
//   - string: Bearer token header (e.g., "Bearer ...").
//   - error: Non-nil if the operation fails, nil on success.
func executeBearerTokenRequest(
	ctx context.Context,
	authURL *url.URL,
	imageName string,
	registryAuth string,
	client Client,
) (string, error) {
	initTokenCache()

	cacheKeyStr := authURL.String() + "|" + registryAuth

	// Attempt cache lookup.
	entry, ok := tokenCache.GetIfPresent(cacheKeyStr)
	if ok {
		if time.Now().Before(entry.expiresAt) {
			logrus.WithFields(logrus.Fields{
				"image": imageName,
				"url":   authURL.String(),
			}).Debug("Using cached bearer token")

			return entry.token, nil
		}

		logrus.WithFields(logrus.Fields{
			"image": imageName,
			"url":   authURL.String(),
		}).Debug("Cached bearer token expired - fetching new token")
	} else {
		logrus.WithFields(logrus.Fields{
			"image": imageName,
			"url":   authURL.String(),
		}).Debug("No cached bearer token - fetching new token")
	}

	// Cache miss or expired: fetch new token.
	token, expiresAt, err := performBearerTokenFetch(
		ctx,
		authURL,
		imageName,
		registryAuth,
		client,
	)
	if err != nil {
		return "", err
	}

	fullToken := "Bearer " + token

	// Populate cache.
	tokenCache.SetIfAbsent(cacheKeyStr, tokenCacheEntry{
		token:     fullToken,
		expiresAt: expiresAt,
	})

	logrus.WithFields(logrus.Fields{
		"image":      imageName,
		"expires_at": expiresAt.Format(time.RFC3339),
	}).Debug("Retrieved and cached bearer token")

	return fullToken, nil
}

// performBearerTokenFetch executes the HTTP request to fetch a bearer token.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - authURL: URL for the bearer token request.
//   - imageName: Image name for logging context.
//   - registryAuth: Base64-encoded auth string.
//   - client: Client for HTTP requests.
//
// Returns:
//   - string: Bearer token value (without "Bearer " prefix).
//   - time.Time: Absolute expiry time for the token.
//   - error: Non-nil if the operation fails, nil on success.
func performBearerTokenFetch(
	ctx context.Context,
	authURL *url.URL,
	imageName string,
	registryAuth string,
	client Client,
) (string, time.Time, error) {
	r, err := newBearerRequest(ctx, authURL, imageName)
	if err != nil {
		return "", time.Time{}, err
	}

	addBasicAuth(r, imageName, registryAuth)
	logrus.WithField("url", r.URL.String()).Debug("Sending bearer token request")

	authResponse, err := client.Do(r)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image": imageName,
			"url":   authURL.String(),
		}).Debug("Failed to execute bearer token request")

		return "", time.Time{}, fmt.Errorf("%w: %w", errFailedExecuteBearerRequest, err)
	}
	defer authResponse.Body.Close()

	// Reject non-success responses before reading the body to avoid
	// attempting to parse error pages as bearer token JSON.
	if authResponse.StatusCode < http.StatusOK || authResponse.StatusCode >= http.StatusMultipleChoices {
		return "", time.Time{}, fmt.Errorf(
			"%w: status %s",
			errFailedExecuteBearerRequest,
			authResponse.Status,
		)
	}

	token, expiresAt, err := readBearerTokenWithExpiry(
		authResponse.Body,
		imageName,
	)
	if err != nil {
		return "", time.Time{}, err
	}

	if token == "" {
		logrus.WithFields(logrus.Fields{
			"image": imageName,
		}).Debug("Empty bearer token received")

		return "", time.Time{}, fmt.Errorf("%w: empty token in response", errFailedDecodeResponse)
	}

	return token, expiresAt, nil
}

// newBearerRequest creates an HTTP request for fetching a bearer token.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - authURL: URL for the bearer token request.
//   - imageName: Image name for logging context.
//
// Returns:
//   - *http.Request: Constructed request if successful.
//   - error: Non-nil if creation fails, nil on success.
func newBearerRequest(ctx context.Context, authURL *url.URL, imageName string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		authURL.String(),
		nil,
	)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image": imageName,
			"url":   authURL.String(),
		}).Debug("Failed to create bearer token request")

		return nil, fmt.Errorf("%w: %w", errFailedCreateBearerRequest, err)
	}

	return request, nil
}

// addBasicAuth adds Basic authentication to the request if credentials are provided.
//
// Parameters:
//   - request: HTTP request to modify.
//   - imageName: Image name for logging context.
//   - registryAuth: Base64-encoded auth string.
func addBasicAuth(request *http.Request, imageName, registryAuth string) {
	if registryAuth != "" {
		logrus.WithFields(logrus.Fields{
			"image": imageName,
		}).Debug("Found credentials")

		if logrus.GetLevel() == logrus.TraceLevel {
			logrus.WithFields(logrus.Fields{
				"image": imageName,
			}).Trace("Using credentials")
		}

		request.Header.Add("Authorization", "Basic "+registryAuth)
	} else {
		logrus.WithFields(logrus.Fields{
			"image": imageName,
		}).Debug("No credentials found")
	}
}

// readBearerTokenWithExpiry reads and parses a bearer token with its expiry time.
//
// Parameters:
//   - body: Response body containing JSON token response.
//   - imageName: Image name for logging context.
//
// Returns:
//   - string: Bearer token value.
//   - time.Time: Absolute expiry time for the token.
//   - error: Non-nil if parsing fails, nil on success.
func readBearerTokenWithExpiry(body io.Reader, imageName string) (string, time.Time, error) {
	const maxBearerTokenResponseSize = 1 << 20

	limitedBody := io.LimitReader(body, maxBearerTokenResponseSize+1)

	b, err := io.ReadAll(limitedBody)
	if err != nil {
		logrus.WithError(err).
			WithField("image", imageName).
			Debug("Failed to read bearer token response body")

		return "", time.Time{}, fmt.Errorf("%w: %w", errFailedDecodeResponse, err)
	}

	if len(b) > maxBearerTokenResponseSize {
		return "", time.Time{}, fmt.Errorf("%w: bearer token response exceeds %d bytes", errFailedDecodeResponse, maxBearerTokenResponseSize)
	}

	tokenResponse := &types.TokenResponse{}

	err = json.Unmarshal(b, tokenResponse)
	if err != nil {
		logrus.WithError(err).
			WithField("image", imageName).
			Debug("Failed to unmarshal bearer token response")

		return "", time.Time{}, fmt.Errorf("%w: %w", errFailedUnmarshalBearerResponse, err)
	}

	expiresAt := computeTokenExpiry(tokenResponse)
	effectiveTTL := int64(time.Until(expiresAt).Round(time.Second).Seconds())
	logrus.WithFields(logrus.Fields{
		"image":              imageName,
		"expires_in":         tokenResponse.ExpiresIn,
		"issued_at":          tokenResponse.IssuedAt,
		"effective_ttl_secs": effectiveTTL,
		"expires_at":         expiresAt.Format(time.RFC3339),
	}).Debug("Parsed bearer token expiry")

	token := tokenResponse.Token
	if token == "" {
		token = tokenResponse.AccessToken
	}

	return token, expiresAt, nil
}

// computeTokenExpiry calculates the absolute expiry time from a token response.
//
// It prefers the registry-provided expires_in combined with issued_at when both
// are available. It falls back to current time when issued_at is absent or invalid,
// and finally defaults to defaultTokenTTL.
//
// Parameters:
//   - tokenResponse: Parsed token response from the registry.
//
// Returns:
//   - time.Time: The absolute expiry time for the token.
func computeTokenExpiry(tokenResponse *types.TokenResponse) time.Time {
	now := time.Now()

	if tokenResponse.IssuedAt != "" {
		issuedAt, parseErr := time.Parse(
			time.RFC3339,
			tokenResponse.IssuedAt,
		)
		if parseErr == nil {
			if tokenResponse.ExpiresIn > 0 {
				return issuedAt.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
			}

			return issuedAt.Add(defaultTokenTTL)
		}

		logrus.WithError(parseErr).
			WithField("issued_at", tokenResponse.IssuedAt).
			Debug("Failed to parse issued_at - using current time as base")
	}

	if tokenResponse.ExpiresIn > 0 {
		return now.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	}

	return now.Add(defaultTokenTTL)
}

// resolveService resolves the service identifier from challenge values,
// deriving it from the realm host when omitted.
//
// Parameters:
//   - values: Parsed challenge values.
//   - imageName: Image name for logging context.
//   - challenge: Raw challenge string for logging context.
//
// Returns:
//   - string: The resolved service identifier.
func resolveService(values challengeValues, imageName, challenge string) string {
	if values.service != "" || values.realm == "" {
		return values.service
	}

	service := extractChallengeHost(values.realm, logrus.Fields{
		"image":     imageName,
		"challenge": challenge,
	})
	if service != "" {
		logrus.WithFields(logrus.Fields{
			"image":   imageName,
			"service": service,
		}).Debug("Derived challenge service from realm")
	}

	return service
}

// validateRequiredChallengeValues ensures realm and service are both present.
//
// Parameters:
//   - values: Parsed challenge values.
//   - imageName: Image name for logging context.
//   - challenge: Raw challenge string for error messages.
//
// Returns:
//   - error: Non-nil if either realm or service is missing.
func validateRequiredChallengeValues(values challengeValues, imageName, challenge string) error {
	if values.realm != "" && values.service != "" {
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"image": imageName,
	}).Warn("Missing required challenge header values: realm or service")

	return fmt.Errorf("%w: missing realm or service in header: %s", errInvalidChallengeHeader, challenge)
}

// buildAuthQuery constructs the query parameters for the auth URL.
//
// It always derives the token scope from the image reference rather than
// using the scope from the WWW-Authenticate challenge. Some registries
// return a placeholder scope in the challenge that their backing token
// service rejects with 403, so the scope is built as:
//
//	repository:<image-path>:pull
//
// Parameters:
//   - authURL: The base auth URL.
//   - values: Parsed challenge values.
//   - imageRef: Normalized image reference.
//
// Returns:
//   - *url.URL: The auth URL with query parameters applied.
func buildAuthQuery(authURL *url.URL, values challengeValues, imageRef reference.Named) *url.URL {
	query := authURL.Query()
	query.Set("service", values.service)

	scopeImage := reference.Path(imageRef)

	scopeRepositoryPrefix := "repository:"
	scopeActionPull := "pull"
	scope := scopeRepositoryPrefix + scopeImage + ":" + scopeActionPull

	logrus.WithFields(logrus.Fields{
		"image": imageRef.Name(),
		"scope": scope,
	}).Debug("Set auth token scope")

	query.Set("scope", scope)
	authURL.RawQuery = query.Encode()

	return authURL
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
	values := parseChallenge(challenge)

	values.service = resolveService(
		values,
		imageRef.Name(),
		challenge,
	)

	logrus.WithFields(logrus.Fields{
		"image":   imageRef.Name(),
		"realm":   values.realm,
		"service": values.service,
		"scope":   values.scope,
	}).Debug("Parsed challenge header")

	err := validateRequiredChallengeValues(
		values,
		imageRef.Name(),
		challenge,
	)
	if err != nil {
		return nil, err
	}

	// Parse the realm into a URL.
	authURL, err := url.Parse(values.realm)
	if err != nil || authURL == nil {
		clog := logrus.WithFields(logrus.Fields{
			"image": imageRef.Name(),
			"realm": values.realm,
		})
		if err != nil {
			clog.WithError(err).Debug("Failed to parse realm URL")
		} else {
			clog.Debug("Invalid realm URL (nil after parsing)")
		}

		return nil, fmt.Errorf("%w: %s", errInvalidRealmURL, values.realm)
	}

	// Reject realms without a host or with a non-HTTP(S) scheme.
	if authURL.Host == "" || (authURL.Scheme != "http" && authURL.Scheme != "https") {
		logrus.WithFields(logrus.Fields{
			"image":  imageRef.Name(),
			"realm":  values.realm,
			"scheme": authURL.Scheme,
			"host":   authURL.Host,
		}).Debug("Invalid realm URL (missing host or unsupported scheme)")

		return nil, fmt.Errorf("%w: %s", errInvalidRealmURL, values.realm)
	}

	authURL = buildAuthQuery(authURL, values, imageRef)

	return authURL, nil
}
