// Package digest provides functionality for retrieving and comparing Docker image digests.
// It supports Watchtower’s update process by checking image freshness via registry HEAD requests
// and fetching digests when needed, handling authentication transformations for compatibility.
package digest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// UserAgent is the User-Agent header value used in HTTP requests.
// It can be set at build time (e.g., via -ldflags) or defaults to "Watchtower/unknown".
var UserAgent = "Watchtower/unknown"

// ContentDigestHeader is the key for the key-value pair containing the digest header.
// It is used to extract the digest from registry HEAD responses.
const ContentDigestHeader = "Docker-Content-Digest"

// minDigestParts is the minimum number of parts expected when splitting a digest string.
// It ensures a digest has at least a prefix and a hash value (e.g., "sha256:hash").
const minDigestParts = 2

// Errors for digest retrieval operations.
// These static errors replace dynamic errors for better error handling discipline.
var (
	errMissingImageInfo        = errors.New("container image info missing")
	errInvalidRegistryResponse = errors.New("registry responded with invalid HEAD request")
)

// manifestResponse represents the JSON response from a registry manifest request.
// It is used to deserialize the digest information when fetching via GET.
type manifestResponse struct {
	Digest string `json:"digest"` // The image digest from the registry
}

// CompareDigest checks if a container’s current image digest matches the latest from the registry.
// It performs a HEAD request to the registry manifest URL, returning true if digests match,
// false if they differ, or an error if the request fails. The registryAuth string is transformed
// to ensure compatibility with various authentication formats.
func CompareDigest(ctx context.Context, container types.Container, registryAuth string) (bool, error) {
	if !container.HasImageInfo() {
		return false, errMissingImageInfo
	}

	registryAuth = TransformAuth(registryAuth)

	token, err := auth.GetToken(ctx, container, registryAuth)
	if err != nil {
		return false, fmt.Errorf("failed to get token: %w", err)
	}

	url, err := manifest.BuildManifestURL(container)
	if err != nil {
		return false, fmt.Errorf("failed to build manifest URL: %w", err)
	}

	logrus.WithField("url", url).Debug("Doing a HEAD request to fetch a digest")

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create HEAD request: %w", err)
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("User-Agent", UserAgent) // Use dynamic User-Agent

	logrus.WithFields(logrus.Fields{
		"method": req.Method,
		"url":    req.URL.String(),
	}).Debug("Sending HEAD request")

	resp, err := auth.Client.Do(req) // Use auth.Client
	if err != nil {
		logrus.WithError(err).Debug("HEAD request failed")

		return false, fmt.Errorf("HEAD request failed: %w", err)
	}
	defer resp.Body.Close()

	logrus.WithFields(logrus.Fields{
		"status":  resp.Status,
		"headers": resp.Header,
	}).Debug("Received HEAD response")

	currentDigest := container.ImageInfo().RepoDigests

	remoteDigest := resp.Header.Get(ContentDigestHeader)
	if remoteDigest == "" {
		wwwAuthHeader := resp.Header.Get("Www-Authenticate")

		return false, fmt.Errorf("%w: status %q, auth: %q", errInvalidRegistryResponse, resp.Status, wwwAuthHeader)
	}

	// Normalize remote digest to match hash-only format
	normalizedRemoteDigest := helpers.NormalizeDigest(remoteDigest)
	logrus.WithField("remote", normalizedRemoteDigest).Debug("Found a remote digest to compare with")

	for _, digest := range currentDigest {
		parts := strings.Split(digest, "@")
		if len(parts) < minDigestParts {
			continue
		}

		// Normalize local digest part to ensure consistent comparison
		normalizedLocalDigest := helpers.NormalizeDigest(parts[1])
		fields := logrus.Fields{"local": normalizedLocalDigest, "remote": normalizedRemoteDigest}
		logrus.WithFields(fields).Debug("Comparing")

		if normalizedRemoteDigest == normalizedLocalDigest {
			logrus.Debug("Found a match")

			return true, nil
		}
	}

	return false, nil
}

// TransformAuth converts a base64-encoded JSON object to a base64-encoded string.
// It decodes the input, extracts credentials, and re-encodes them as "username:password"
// for compatibility with registry authentication headers.
func TransformAuth(registryAuth string) string {
	b, _ := base64.StdEncoding.DecodeString(registryAuth)
	credentials := &types.RegistryCredentials{}
	_ = json.Unmarshal(b, credentials) //nolint:musttag

	if credentials.Username != "" && credentials.Password != "" {
		ba := fmt.Appendf(nil, "%s:%s", credentials.Username, credentials.Password)
		registryAuth = base64.StdEncoding.EncodeToString(ba)
	}

	return registryAuth
}

// FetchDigest retrieves the digest of an image from its registry.
// It performs a GET request to the manifest URL, extracts the digest from the response,
// and normalizes it for consistency across registry formats.
func FetchDigest(ctx context.Context, container types.Container, authToken string) (string, error) {
	token, err := auth.GetToken(ctx, container, authToken)
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	url, err := manifest.BuildManifestURL(container)
	if err != nil {
		return "", fmt.Errorf("failed to build manifest URL: %w", err)
	}

	logrus.WithField("url", url).Debug("Doing a GET request to fetch a digest")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create GET request: %w", err)
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("User-Agent", UserAgent)

	logrus.WithFields(logrus.Fields{
		"method": req.Method,
		"url":    req.URL.String(),
	}).Debug("Sending GET request")

	resp, err := auth.Client.Do(req)
	if err != nil {
		logrus.WithError(err).Debug("GET request failed")

		return "", fmt.Errorf("GET request failed: %w", err)
	}
	defer resp.Body.Close()

	var response manifestResponse

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		logrus.WithError(err).Debug("Failed to decode manifest response")

		return "", fmt.Errorf("failed to decode manifest response: %w", err)
	}

	normalizedDigest := helpers.NormalizeDigest(response.Digest)
	logrus.WithField("digest", normalizedDigest).Debug("Fetched and normalized digest from registry")

	return normalizedDigest, nil
}
