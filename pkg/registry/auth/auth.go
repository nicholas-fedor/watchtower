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

// Static errors for registry authentication failures.
var (
	errNoCredentials          = errors.New("no credentials available")
	errUnsupportedChallenge   = errors.New("unsupported challenge type from registry")
	errInvalidChallengeHeader = errors.New("challenge header did not include all values needed to construct an auth url")
	errInvalidRealmURL        = errors.New("invalid realm URL in challenge header")
)

// GetToken fetches a token for the registry hosting the provided image.
// It uses the provided registry authentication string to obtain the token,
// propagating the provided context for request cancellation and timeouts.
func GetToken(ctx context.Context, container types.Container, registryAuth string) (string, error) {
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		return "", fmt.Errorf("failed to parse image name: %w", err)
	}

	url := GetChallengeURL(normalizedRef)
	logrus.WithField("URL", url.String()).Debug("Built challenge URL")

	req, err := GetChallengeRequest(ctx, url)
	if err != nil {
		return "", err
	}

	res, err := Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute challenge request: %w", err)
	}
	defer res.Body.Close()

	values := res.Header.Get(ChallengeHeader)
	logrus.WithFields(logrus.Fields{
		"status": res.Status,
		"header": values,
	}).Debug("Got response to challenge request")

	challenge := strings.ToLower(values)
	if strings.HasPrefix(challenge, "basic") {
		if registryAuth == "" {
			return "", errNoCredentials
		}

		return "Basic " + registryAuth, nil
	}

	if strings.HasPrefix(challenge, "bearer") {
		return GetBearerHeader(ctx, challenge, normalizedRef, registryAuth)
	}

	return "", errUnsupportedChallenge
}

// GetChallengeRequest creates a request for getting challenge instructions.
// It constructs an HTTP GET request with appropriate headers for the registry challenge,
// using the provided context for cancellation and timeouts.
func GetChallengeRequest(ctx context.Context, url url.URL) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create challenge request: %w", err)
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Watchtower (Docker)")

	return req, nil
}

// GetBearerHeader tries to fetch a bearer token from the registry based on the challenge instructions.
// It uses the provided registry authentication string if available, propagating the context
// for request cancellation and timeouts.
func GetBearerHeader(ctx context.Context, challenge string, imageRef reference.Named, registryAuth string) (string, error) {
	authURL, err := GetAuthURL(challenge, imageRef)
	if err != nil {
		return "", err
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create bearer token request: %w", err)
	}

	if registryAuth != "" {
		logrus.Debug("Credentials found.")
		// CREDENTIAL: Uncomment to log registry credentials
		// logrus.Tracef("Credentials: %v", registryAuth)
		r.Header.Add("Authorization", "Basic "+registryAuth)
	} else {
		logrus.Debug("No credentials found.")
	}

	authResponse, err := Client.Do(r)
	if err != nil {
		return "", fmt.Errorf("failed to execute bearer token request: %w", err)
	}
	defer authResponse.Body.Close()

	body, _ := io.ReadAll(authResponse.Body)
	tokenResponse := &types.TokenResponse{}

	err = json.Unmarshal(body, tokenResponse)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal bearer token response: %w", err)
	}

	return "Bearer " + tokenResponse.Token, nil
}

// GetAuthURL constructs an authentication URL from the challenge instructions.
// It parses the challenge header and builds a URL with service and scope parameters.
func GetAuthURL(challenge string, imageRef reference.Named) (*url.URL, error) {
	loweredChallenge := strings.ToLower(challenge)
	raw := strings.TrimPrefix(loweredChallenge, "bearer")

	pairs := strings.Split(raw, ",")
	values := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		trimmed := strings.Trim(pair, " ")
		if key, val, ok := strings.Cut(trimmed, "="); ok {
			values[key] = strings.Trim(val, `"`)
		}
	}

	logrus.WithFields(logrus.Fields{
		"realm":   values["realm"],
		"service": values["service"],
	}).Debug("Checking challenge header content")

	if values["realm"] == "" || values["service"] == "" {
		return nil, errInvalidChallengeHeader
	}

	authURL, err := url.Parse(values["realm"])
	if err != nil || authURL == nil {
		return nil, fmt.Errorf("%w: %s", errInvalidRealmURL, values["realm"])
	}

	query := authURL.Query()
	query.Add("service", values["service"])

	scopeImage := reference.Path(imageRef)
	scope := fmt.Sprintf("repository:%s:pull", scopeImage)
	logrus.WithFields(logrus.Fields{
		"scope": scope,
		"image": imageRef.Name(),
	}).Debug("Setting scope for auth token")
	query.Add("scope", scope)

	authURL.RawQuery = query.Encode()

	return authURL, nil
}

// GetChallengeURL generates a challenge URL for accessing a given image’s registry.
// It constructs a base URL using the registry address derived from the image reference.
func GetChallengeURL(imageRef reference.Named) url.URL {
	host, _ := helpers.GetRegistryAddress(imageRef.Name())

	URL := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   "/v2/",
	}

	return URL
}
