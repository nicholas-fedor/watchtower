// Package auth provides functionality for authenticating with container registries.
// It handles token retrieval and challenge URL generation for registry access.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/distribution/reference"
	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
)

// ChallengeHeader is the HTTP Header containing challenge instructions.
const ChallengeHeader = "WWW-Authenticate"

// Client is the HTTP client used for making requests to registries.
// It is exposed at the package level to allow customization (e.g., in tests).
var Client = &http.Client{}

// Static errors for registry authentication failures.
var (
	errNoCredentials          = errors.New("no credentials available")
	errUnsupportedChallenge   = errors.New("unsupported challenge type from registry")
	errInvalidChallengeHeader = errors.New("challenge header did not include all values needed to construct an auth url")
)

// GetToken fetches a token for the registry hosting the provided image.
// It uses the provided registry authentication string to obtain the token.
func GetToken(container types.Container, registryAuth string) (string, error) {
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		return "", err
	}

	url := GetChallengeURL(normalizedRef)
	logrus.WithField("URL", url.String()).Debug("Built challenge URL")

	req, err := GetChallengeRequest(url)
	if err != nil {
		return "", err
	}

	res, err := Client.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()
	v := res.Header.Get(ChallengeHeader)

	logrus.WithFields(logrus.Fields{
		"status": res.Status,
		"header": v,
	}).Debug("Got response to challenge request")

	challenge := strings.ToLower(v)
	if strings.HasPrefix(challenge, "basic") {
		if registryAuth == "" {
			return "", errNoCredentials
		}

		return "Basic " + registryAuth, nil
	}

	if strings.HasPrefix(challenge, "bearer") {
		return GetBearerHeader(challenge, normalizedRef, registryAuth)
	}

	return "", errUnsupportedChallenge
}

// GetChallengeRequest creates a request for getting challenge instructions.
// It constructs an HTTP GET request with appropriate headers for the registry challenge.
func GetChallengeRequest(url url.URL) (*http.Request, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Watchtower (Docker)")

	return req, nil
}

// GetBearerHeader tries to fetch a bearer token from the registry based on the challenge instructions.
// It uses the provided registry authentication string if available.
func GetBearerHeader(challenge string, imageRef reference.Named, registryAuth string) (string, error) {
	authURL, err := GetAuthURL(challenge, imageRef)
	if err != nil {
		return "", err
	}

	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, authURL.String(), nil)
	if err != nil {
		return "", err
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
		return "", err
	}
	defer authResponse.Body.Close()

	body, _ := io.ReadAll(authResponse.Body)
	tokenResponse := &types.TokenResponse{}

	err = json.Unmarshal(body, tokenResponse)
	if err != nil {
		return "", err
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

	authURL, _ := url.Parse(values["realm"])
	q := authURL.Query()
	q.Add("service", values["service"])

	scopeImage := reference.Path(imageRef)

	scope := fmt.Sprintf("repository:%s:pull", scopeImage)
	logrus.WithFields(logrus.Fields{"scope": scope, "image": imageRef.Name()}).Debug("Setting scope for auth token")
	q.Add("scope", scope)

	authURL.RawQuery = q.Encode()

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
