// Package digest provides functionality for retrieving and comparing Docker image digests in Watchtower.
// It enables the update process by fetching digests from container registries using HTTP requests,
// comparing them with local image digests, and handling authentication transformations to ensure compatibility
// with various registry authentication schemes. This package is integral to determining whether a container’s
// image is stale and requires an update.
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

// ContentDigestHeader is the HTTP header key used to retrieve the digest from a registry’s response.
// This header, typically "Docker-Content-Digest", contains the digest value (e.g., "sha256:abc...") for an image manifest,
// allowing Watchtower to compare or fetch it without downloading the full manifest body.
const ContentDigestHeader = "Docker-Content-Digest"

// minDigestParts defines the minimum number of parts expected when splitting a digest string.
// A valid digest typically has two parts: a prefix (e.g., "sha256") and a hash value (e.g., "abc..."), separated by a colon.
// This constant ensures digest strings are well-formed before comparison or processing.
const minDigestParts = 2

// UserAgent is the User-Agent header value used in HTTP requests to identify Watchtower as the client.
// It can be customized at build time using linker flags (e.g., -ldflags "-X ...UserAgent=Watchtower/v1.0").
// If not set during the build, it defaults to "Watchtower/unknown", providing a fallback identifier for registry requests.
var UserAgent = "Watchtower/unknown"

// Errors for digest retrieval operations.
var (
	// errMissingImageInfo indicates the container lacks image metadata, preventing digest operations.
	errMissingImageInfo = errors.New("container image info missing")
	// errInvalidRegistryResponse indicates the registry’s HEAD response lacks a digest or is malformed.
	errInvalidRegistryResponse = errors.New("registry responded with invalid HEAD request")
	// errDigestExtractionFailed indicates a failure to extract a digest from a GET response due to decoding issues.
	errDigestExtractionFailed = errors.New("failed to extract digest from response")
	// errFailedGetToken indicates a failure to obtain an authentication token from the registry.
	errFailedGetToken = errors.New("failed to get token")
	// errFailedBuildManifestURL indicates a failure to construct the manifest URL for the registry.
	errFailedBuildManifestURL = errors.New("failed to build manifest URL")
	// errFailedCreateRequest indicates a failure to construct an HTTP request for digest retrieval.
	errFailedCreateRequest = errors.New("failed to create request")
	// errFailedExecuteRequest indicates a failure to execute an HTTP request to the registry.
	errFailedExecuteRequest = errors.New("failed to execute request")
)

// manifestResponse represents the JSON structure of a registry manifest response from a GET request.
// It is used to deserialize the digest field when fetching the full manifest body, providing a structured way
// to access the image digest returned by the registry.
type manifestResponse struct {
	// Digest is the image digest from the registry, typically in the format "sha256:abc...".
	Digest string `json:"digest"`
}

// CompareDigest checks whether a container’s current image digest matches the latest digest from its registry.
// It performs a HEAD request to retrieve the remote digest efficiently without downloading the full manifest,
// then compares it against the container’s local digests. This function is critical for determining if an image update
// is needed during Watchtower’s update process.
//
// Parameters:
//   - ctx: The context controlling the request’s lifecycle, allowing cancellation or timeouts.
//   - container: The container whose image digest is being compared, providing local digest information.
//   - registryAuth: A base64-encoded authentication string (e.g., JSON credentials or "username:password").
//
// Returns:
//   - bool: True if any local digest matches the remote digest, indicating the image is up-to-date; false otherwise.
//   - error: An error if the operation fails (e.g., missing image info, request failure), nil if successful.
func CompareDigest(
	ctx context.Context,
	container types.Container,
	registryAuth string,
) (bool, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Ensure the container has image metadata to proceed with digest comparison.
	if !container.HasImageInfo() {
		logrus.WithFields(fields).Debug("Container image info missing")

		return false, errMissingImageInfo
	}

	// Fetch the latest digest from the registry using a HEAD request for efficiency.
	remoteDigest, err := fetchDigest(ctx, container, registryAuth, http.MethodHead)
	if err != nil {
		return false, err
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"remote_digest": remoteDigest,
	}).Debug("Fetched remote digest")

	// Compare the fetched remote digest with the container’s local digests.
	matches := digestsMatch(container.ImageInfo().RepoDigests, remoteDigest)

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"matches": matches,
	}).Debug("Completed digest comparison")

	return matches, nil
}

// FetchDigest retrieves the digest of an image from its registry using a GET request.
// Unlike CompareDigest, it fetches the full manifest body to extract the digest, which may be necessary when
// HEAD requests are unsupported or additional metadata is required. The digest is normalized for consistency.
//
// Parameters:
//   - ctx: The context controlling the request’s lifecycle, enabling cancellation or timeouts.
//   - container: The container whose image digest is being fetched, providing the image name and reference.
//   - authToken: A base64-encoded authentication string for registry access.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if successful.
//   - error: An error if the request or decoding fails, nil if successful.
func FetchDigest(ctx context.Context, container types.Container, authToken string) (string, error) {
	return fetchDigest(ctx, container, authToken, http.MethodGet)
}

// fetchDigest retrieves an image digest from the registry using the specified HTTP method (HEAD or GET).
// It orchestrates authentication, request construction, and digest extraction, providing a unified approach
// for both comparison (HEAD) and full fetch (GET) operations. This function reduces code duplication by
// handling the common logic for both CompareDigest and FetchDigest.
//
// Parameters:
//   - ctx: The context controlling the request’s lifecycle, supporting cancellation and timeouts.
//   - container: The container whose image digest is being retrieved, used for URL and auth construction.
//   - registryAuth: The base64-encoded authentication string, transformed as needed.
//   - method: The HTTP method ("HEAD" or "GET") determining how the digest is fetched.
//
// Returns:
//   - string: The normalized digest extracted from the response.
//   - error: An error if any step (auth, URL, request, extraction) fails, nil if successful.
func fetchDigest(
	ctx context.Context,
	container types.Container,
	registryAuth, method string,
) (string, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Transform the provided auth string into a usable format for registry authentication.
	registryAuth = TransformAuth(registryAuth)

	// Obtain an authentication token for the registry, leveraging the container’s image reference.
	token, err := auth.GetToken(ctx, container, registryAuth)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get token")

		return "", fmt.Errorf("%w: %w", errFailedGetToken, err)
	}

	// Build the manifest URL based on the container’s image name and tag.
	url, err := manifest.BuildManifestURL(container)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to build manifest URL")

		return "", fmt.Errorf("%w: %w", errFailedBuildManifestURL, err)
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"method": method,
		"url":    url,
	}).Debug("Fetching digest")

	// Construct the HTTP request with the appropriate method, headers, and context.
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithFields(logrus.Fields{
			"method": method,
			"url":    url,
		}).Debug("Failed to create request")

		return "", fmt.Errorf("%w: %w", errFailedCreateRequest, err)
	}

	// Set standard headers for Docker registry manifest requests.
	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("User-Agent", UserAgent)

	// Execute the request using the configured auth client.
	resp, err := auth.Client.Do(req)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithFields(logrus.Fields{
			"method": method,
			"url":    url,
		}).Debug("Failed to execute request")

		return "", fmt.Errorf("%w: %w", errFailedExecuteRequest, err)
	}
	defer resp.Body.Close()

	// Extract the digest based on the request method (HEAD from headers, GET from body).
	if method == http.MethodHead {
		return extractHeadDigest(resp)
	}

	return extractGetDigest(resp)
}

// extractHeadDigest extracts the image digest from a HEAD response’s headers.
// It retrieves the digest from the "Docker-Content-Digest" header, normalizing it for comparison,
// and validates its presence to ensure a valid response from the registry.
//
// Parameters:
//   - resp: The HTTP response from a HEAD request containing headers.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if present.
//   - error: An error if the digest is missing or the response is invalid, nil if successful.
func extractHeadDigest(resp *http.Response) (string, error) {
	digest := resp.Header.Get(ContentDigestHeader)
	if digest == "" {
		wwwAuthHeader := resp.Header.Get("Www-Authenticate")
		logrus.WithFields(logrus.Fields{
			"status":      resp.Status,
			"auth_header": wwwAuthHeader,
		}).Debug("Registry responded with invalid HEAD request")

		return "", fmt.Errorf(
			"%w: status %q, auth: %q",
			errInvalidRegistryResponse,
			resp.Status,
			wwwAuthHeader,
		)
	}

	normalizedDigest := helpers.NormalizeDigest(digest)
	logrus.WithFields(logrus.Fields{
		"digest": normalizedDigest,
	}).Debug("Extracted digest from HEAD response")

	return normalizedDigest, nil
}

// extractGetDigest extracts the image digest from a GET response’s body.
// It decodes the JSON manifest response to retrieve the digest field, normalizing it for consistency,
// and handles decoding errors gracefully.
//
// Parameters:
//   - resp: The HTTP response from a GET request containing the manifest body.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if successful.
//   - error: An error if decoding fails or the digest is missing, nil if successful.
func extractGetDigest(resp *http.Response) (string, error) {
	var response manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logrus.WithError(err).Debug("Failed to extract digest from GET response")

		return "", fmt.Errorf("%w: %w", errDigestExtractionFailed, err)
	}

	normalizedDigest := helpers.NormalizeDigest(response.Digest)
	logrus.WithFields(logrus.Fields{
		"digest": normalizedDigest,
	}).Debug("Extracted digest from GET response")

	return normalizedDigest, nil
}

// digestsMatch compares a list of local digests with a remote digest to determine if there’s a match.
// It normalizes both the remote digest and each local digest, checking for equality to confirm
// whether the container’s image is up-to-date with the registry’s latest version.
//
// Parameters:
//   - localDigests: A slice of local digests from the container’s image info (e.g., "sha256:abc...").
//   - remoteDigest: The digest fetched from the registry (e.g., "sha256:abc..." or raw hash).
//
// Returns:
//   - bool: True if any normalized local digest matches the normalized remote digest, false otherwise.
func digestsMatch(localDigests []string, remoteDigest string) bool {
	normalizedRemoteDigest := helpers.NormalizeDigest(remoteDigest)

	for _, digest := range localDigests {
		parts := strings.Split(digest, "@")
		if len(parts) < minDigestParts {
			continue
		}

		normalizedLocalDigest := helpers.NormalizeDigest(parts[1])
		logrus.WithFields(logrus.Fields{
			"local_digest":  normalizedLocalDigest,
			"remote_digest": normalizedRemoteDigest,
		}).Debug("Comparing digests")

		if normalizedLocalDigest == normalizedRemoteDigest {
			logrus.Debug("Found digest match")

			return true
		}
	}

	return false
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

	// Unmarshal the decoded bytes into a credentials struct, ignoring errors as per original behavior.
	_ = json.Unmarshal(b, credentials) //nolint:musttag

	// If both username and password are present, re-encode them as "username:password".
	if credentials.Username != "" && credentials.Password != "" {
		ba := fmt.Appendf(nil, "%s:%s", credentials.Username, credentials.Password)
		registryAuth = base64.StdEncoding.EncodeToString(ba)
	}

	return registryAuth
}
