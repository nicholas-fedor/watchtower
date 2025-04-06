// Package auth provides functionality for authenticating with container registries.
// It handles token retrieval and challenge URL generation for registry access.
package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ChallengeHeader is the HTTP Header containing challenge instructions.
const ChallengeHeader = "WWW-Authenticate"

// Constants for HTTP client configuration.
// These values define timeouts and connection limits for registry requests.
const (
	DefaultTimeoutSeconds             = 30  // Default timeout for HTTP requests in seconds
	DefaultMaxIdleConns               = 100 // Maximum number of idle connections in the pool
	DefaultIdleConnTimeoutSeconds     = 90  // Timeout for idle connections in seconds
	DefaultTLSHandshakeTimeoutSeconds = 10  // Timeout for TLS handshake in seconds
	DefaultExpectContinueTimeout      = 1
)

// Client is the HTTP client used for making requests to registries.
// It is exposed at the package level to allow customization (e.g., in tests).
var Client = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:          DefaultMaxIdleConns,
		IdleConnTimeout:       DefaultIdleConnTimeoutSeconds * time.Second,
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeoutSeconds * time.Second,
		ExpectContinueTimeout: DefaultExpectContinueTimeout * time.Second,
	},
	Timeout: DefaultTimeoutSeconds * time.Second,
}

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
)

// GetToken fetches a token for the registry hosting the provided image.
//
// It uses the registry authentication string to obtain the token, respecting the context for cancellation.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - container: Container with image info for token retrieval.
//   - registryAuth: Base64-encoded auth string (e.g., "username:password").
//
// Returns:
//   - string: Authentication token (e.g., "Basic ..." or "Bearer ...") if successful.
//   - error: Non-nil if the operation fails, nil on success.
func GetToken(ctx context.Context, container types.Container, registryAuth string) (string, error) {
	// Parse image name into a normalized reference for registry access.
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).
			WithField("image", container.ImageName()).
			Debug("Failed to parse image name")

		return "", fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	// Generate the challenge URL to initiate authentication.
	url := GetChallengeURL(normalizedRef)
	logrus.WithFields(logrus.Fields{
		"image": container.ImageName(),
		"url":   url.String(),
	}).Debug("Constructed challenge URL")

	// Build and execute the challenge request to get auth instructions.
	req, err := GetChallengeRequest(ctx, url)
	if err != nil {
		return "", err
	}

	res, err := Client.Do(req)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image": container.ImageName(),
			"url":   url.String(),
		}).Debug("Failed to execute challenge request")

		return "", fmt.Errorf("%w: %w", errFailedExecuteChallengeRequest, err)
	}
	defer res.Body.Close()

	// Extract and process the challenge header.
	values := res.Header.Get(ChallengeHeader)
	logrus.WithFields(logrus.Fields{
		"image":  container.ImageName(),
		"status": res.Status,
		"header": values,
	}).Debug("Received challenge response")

	challenge := strings.ToLower(values)
	logrus.WithFields(logrus.Fields{
		"image":     container.ImageName(),
		"challenge": challenge,
	}).Debug("Processing challenge type")

	// Handle Basic auth if specified.
	if strings.HasPrefix(challenge, "basic") {
		if registryAuth == "" {
			return "", errNoCredentials // No creds provided for Basic auth.
		}

		return "Basic " + registryAuth, nil // Return pre-formatted Basic auth header.
	}

	// Handle Bearer auth by fetching a token.
	if strings.HasPrefix(challenge, "bearer") {
		return GetBearerHeader(ctx, challenge, normalizedRef, registryAuth)
	}

	// Unknown challenge type encountered.
	logrus.WithFields(logrus.Fields{
		"image":     container.ImageName(),
		"challenge": challenge,
	}).Error("Unsupported challenge type from registry")

	return "", errUnsupportedChallenge
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
//   - ctx: Context for request lifecycle control.
//   - challenge: Challenge string from the registry.
//   - imageRef: Normalized image reference.
//   - registryAuth: Base64-encoded auth string.
//
// Returns:
//   - string: Bearer token header (e.g., "Bearer ...") if successful.
//   - error: Non-nil if the operation fails, nil on success.
func GetBearerHeader(
	ctx context.Context,
	challenge string,
	imageRef reference.Named,
	registryAuth string,
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
	authResponse, err := Client.Do(r)
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

	logrus.WithFields(logrus.Fields{
		"image":   imageRef.Name(),
		"realm":   values["realm"],
		"service": values["service"],
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

// GetChallengeURL generates a challenge URL for accessing an image’s registry.
//
// Parameters:
//   - imageRef: Normalized image reference.
//
// Returns:
//   - url.URL: Generated challenge URL.
func GetChallengeURL(imageRef reference.Named) url.URL {
	// Extract registry host from the image reference.
	host, _ := helpers.GetRegistryAddress(imageRef.Name())
	URL := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   "/v2/",
	}
	logrus.WithFields(logrus.Fields{
		"image": imageRef.Name(),
		"url":   URL.String(),
	}).Debug("Generated challenge URL")

	return URL
}
