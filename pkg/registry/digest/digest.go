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
	"net/url"
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

const (
	// minDigestParts defines the minimum number of parts expected when splitting a digest string.
	// A valid digest typically has two parts: a prefix (e.g., "sha256") and a hash value (e.g., "abc..."), separated by a colon.
	// This constant ensures digest strings are well-formed before comparison or processing.
	minDigestParts = 2
)

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
//
// It is used to deserialize the digest field when fetching the full manifest body, providing a structured way
// to access the image digest returned by the registry.
type manifestResponse struct {
	// Digest is the image digest from the registry, typically in the format "sha256:abc...".
	Digest string `json:"digest"`
}

// CompareDigest checks whether a container’s current image digest matches the latest from its registry.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - container: Container whose digest is being compared.
//   - registryAuth: Base64-encoded auth string.
//
// Returns:
//   - bool: True if digests match (image is up-to-date), false otherwise.
//   - error: Non-nil if operation fails, nil on success.
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

	// If HEAD request returned empty digest (due to 404), fall back to GET request.
	if remoteDigest == "" {
		logrus.WithFields(fields).Debug("HEAD request returned empty digest, falling back to GET")

		remoteDigest, err = FetchDigest(ctx, container, registryAuth)
		if err != nil {
			return false, err
		}
	}

	logrus.WithFields(fields).
		WithField("remote_digest", remoteDigest).
		Debug("Fetched remote digest")

	// Compare the fetched remote digest with the container’s local digests.
	matches := digestsMatch(container.ImageInfo().RepoDigests, remoteDigest)
	logrus.WithFields(fields).WithField("matches", matches).Debug("Completed digest comparison")

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

// fetchDigest retrieves an image digest using the specified HTTP method.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - container: Container whose digest is being retrieved.
//   - registryAuth: Base64-encoded auth string.
//   - method: HTTP method ("HEAD" or "GET").
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil if operation fails, nil on success.
func fetchDigest(
	ctx context.Context,
	container types.Container,
	registryAuth string,
	method string,
) (string, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Transform the provided auth string into a usable format for registry authentication.
	registryAuth = TransformAuth(registryAuth)

	// Create an authentication client for registry requests.
	client := auth.NewAuthClient()

	// Build the initial manifest URL based on the container’s image name and tag.
	manifestURL, err := manifest.BuildManifestURL(container)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to build manifest URL")

		return "", fmt.Errorf("%w: %w", errFailedBuildManifestURL, err)
	}

	// Parse the initial manifest URL to extract the original host.
	parsedURL, err := url.Parse(manifestURL)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to parse initial manifest URL")

		return "", fmt.Errorf(
			"%w: failed to parse manifest URL: %w",
			errFailedBuildManifestURL,
			err,
		)
	}

	if parsedURL.Host == "" {
		logrus.WithFields(fields).
			WithField("url", manifestURL).
			Debug("Parsed manifest URL has no host")

		return "", fmt.Errorf(
			"%w: manifest URL has no host: %s",
			errFailedBuildManifestURL,
			manifestURL,
		)
	}

	originalHost := parsedURL.Host
	logrus.WithFields(fields).
		WithField("original_host", originalHost).
		Debug("Extracted original host from manifest URL")

	// Obtain an authentication token and challenge host for the registry.
	token, challengeHost, err := auth.GetToken(ctx, container, registryAuth, client)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get token")

		return "", fmt.Errorf("%w: %w", errFailedGetToken, err)
	}

	// If no token is returned, authentication is not required.
	if token == "" {
		logrus.WithFields(fields).Debug("No authentication required, proceeding with request")
	} else {
		logrus.WithFields(fields).
			WithField("challenge_host", challengeHost).
			Debug("Received challenge host from GetToken")
	}

	// If the challenge response indicates a different host (e.g., due to a proxy redirect),
	// reconstruct the manifest URL using the challenge host.
	if challengeHost != "" && challengeHost != originalHost {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"original_host":  originalHost,
			"challenge_host": challengeHost,
		}).Debug("Detected registry redirect, updating manifest URL host")

		parsedURL.Host = challengeHost
		manifestURL = parsedURL.String()
		logrus.WithFields(fields).
			WithField("updated_url", manifestURL).
			Debug("Reconstructed manifest URL")
	} else {
		logrus.WithFields(fields).
			WithField("challenge_host", challengeHost).
			WithField("original_host", originalHost).
			Debug("No manifest URL update needed; challenge host empty or same as original")
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"method": method,
		"url":    manifestURL,
	}).Debug("Fetching digest")

	// Construct the HTTP request with the appropriate method, headers, and context.
	req, err := http.NewRequestWithContext(ctx, method, manifestURL, nil)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithFields(logrus.Fields{
			"method": method,
			"url":    manifestURL,
		}).Debug("Failed to create request")

		return "", fmt.Errorf("%w: %w", errFailedCreateRequest, err)
	}

	// Set headers only if a token is provided.
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	// Set Accept header to support both OCI image indexes and Docker V2 manifests.
	req.Header.Set(
		"Accept",
		"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json",
	)
	req.Header.Set("User-Agent", UserAgent)

	// Execute the request using the provided authentication client.
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithFields(logrus.Fields{
			"method": method,
			"url":    manifestURL,
		}).Debug("Failed to execute request")

		return "", fmt.Errorf("%w: %w", errFailedExecuteRequest, err)
	}
	defer resp.Body.Close()

	// Handle 404 response for HEAD requests, assuming no update is needed.
	if method == http.MethodHead && resp.StatusCode == http.StatusNotFound {
		logrus.WithFields(fields).WithField("status", resp.Status).
			Debug("Registry returned 404 for HEAD request, assuming no update needed")

		return "", nil // Treat as no update available
	}

	// Extract the digest based on the request method (HEAD from headers, GET from body).
	var digest string
	if method == http.MethodHead {
		digest, err = extractHeadDigest(resp)
	} else {
		digest, err = extractGetDigest(resp)
	}

	if err != nil {
		logrus.WithError(err).WithFields(fields).WithField("status", resp.Status).
			Debug("Failed to extract digest")

		return "", err
	}

	logrus.WithFields(fields).WithField("remote_digest", digest).
		Debug("Fetched remote digest")

	return digest, nil
}

// extractHeadDigest extracts the image digest from a HEAD response’s headers.
//
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
	// Retrieve the digest from the standard header.
	digest := resp.Header.Get(ContentDigestHeader)
	if digest == "" {
		// Log and return an error if the digest is missing, including auth details for debugging.
		wwwAuthHeader := resp.Header.Get("www-authenticate")
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

	// Normalize the digest (e.g., strip "sha256:") for consistency.
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
	// Decode the JSON response body into the manifest structure.
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logrus.WithError(err).Debug("Failed to extract digest from GET response")

		return "", fmt.Errorf("%w: %w", errDigestExtractionFailed, err)
	}

	// Normalize the extracted digest for consistent comparison.
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
	// Normalize the remote digest once for efficiency.
	normalizedRemoteDigest := helpers.NormalizeDigest(remoteDigest)

	for _, digest := range localDigests {
		// Split digest into repo and hash parts (e.g., "repo@sha256:abc").
		parts := strings.Split(digest, "@")
		if len(parts) < minDigestParts {
			continue // Skip malformed digests.
		}

		// Normalize the local digest’s hash part.
		normalizedLocalDigest := helpers.NormalizeDigest(parts[1])
		logrus.WithFields(logrus.Fields{
			"local_digest":  normalizedLocalDigest,
			"remote_digest": normalizedRemoteDigest,
		}).Debug("Comparing digests")

		// Return true on the first match.
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

	// Unmarshal JSON into credentials struct, ignoring errors if malformed.
	_ = json.Unmarshal(b, credentials) //nolint:musttag

	// If both username and password are present, re-encode them as "username:password".
	if credentials.Username != "" && credentials.Password != "" {
		ba := fmt.Appendf(nil, "%s:%s", credentials.Username, credentials.Password)
		registryAuth = base64.StdEncoding.EncodeToString(ba)
	}

	return registryAuth // Return original if no valid credentials.
}
