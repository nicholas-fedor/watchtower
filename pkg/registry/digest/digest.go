// Package digest provides functionality for retrieving and comparing Docker image digests in Watchtower.
// It enables the update process by fetching digests from container registries using HTTP requests,
// comparing them with local image digests, and handling authentication transformations to ensure compatibility
// with various registry authentication schemes. This package is integral to determining whether a container’s
// image is stale and requires an update.
package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
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
	// errFailedGetToken indicates a failure to obtain an authentication token from the registry.
	errFailedGetToken = errors.New("failed to get token")
	// errFailedBuildManifestURL indicates a failure to construct the manifest URL for the registry.
	errFailedBuildManifestURL = errors.New("failed to build manifest URL")
	// errFailedCreateRequest indicates a failure to construct an HTTP request for digest retrieval.
	errFailedCreateRequest = errors.New("failed to create request")
	// errFailedExecuteRequest indicates a failure to execute an HTTP request to the registry.
	errFailedExecuteRequest = errors.New("failed to execute request")
)

// NormalizeDigest standardizes a digest string for consistent comparison.
//
// It trims common prefixes (e.g., "sha256:") to return the raw digest value.
//
// Parameters:
//   - digest: Digest string (e.g., "sha256:abc123").
//
// Returns:
//   - string: Normalized digest (e.g., "abc123").
func NormalizeDigest(digest string) string {
	// List of prefixes to strip from the digest.
	prefixes := []string{"sha256:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(digest, prefix) {
			// Trim the prefix to get the raw digest value.
			normalized := strings.TrimPrefix(digest, prefix)
			logrus.WithFields(logrus.Fields{
				"original":   digest,
				"normalized": normalized,
			}).Debug("Normalized digest by trimming prefix")

			return normalized
		}
	}

	// Return unchanged if no prefix matches.
	return digest
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
// It fetches the manifest to ensure the digest header is present, which may be necessary when
// HEAD requests are unsupported. The digest is extracted from the response headers and normalized for consistency.
//
// Parameters:
//   - ctx: The context controlling the request’s lifecycle, enabling cancellation or timeouts.
//   - container: The container whose image digest is being fetched, providing the image name and reference.
//   - authToken: A base64-encoded authentication string for registry access.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if successful.
//   - error: An error if the request fails or digest header is missing, nil if successful.
func FetchDigest(ctx context.Context, container types.Container, authToken string) (string, error) {
	return fetchDigest(ctx, container, authToken, http.MethodGet)
}

// buildManifestURL constructs and validates the manifest URL for a container.
//
// It determines the appropriate scheme based on WATCHTOWER_REGISTRY_TLS_SKIP,
// builds the initial manifest URL using the container's image information,
// parses it to extract the host, optionally overrides the host if provided,
// and validates that the host is present.
//
// Parameters:
//   - container: Container whose manifest URL is being built.
//   - hostOverride: Optional host to override the parsed host (empty string to use original).
//
// Returns:
//   - string: The constructed manifest URL.
//   - string: The original host extracted from the URL (before override).
//   - *url.URL: Parsed URL object.
//   - error: Non-nil if URL construction or validation fails, nil on success.
func buildManifestURL(
	container types.Container,
	hostOverride string,
) (string, string, *url.URL, error) {
	// Determine scheme based on WATCHTOWER_REGISTRY_TLS_SKIP.
	scheme := "https"
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		scheme = "http"
	}

	// Build the initial manifest URL based on the container's image name and tag.
	manifestURL, err := manifest.BuildManifestURL(container, scheme)
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: %w", errFailedBuildManifestURL, err)
	}

	// Parse the initial manifest URL to extract the original host.
	parsedURL, err := url.Parse(manifestURL)
	if err != nil {
		return "", "", nil, fmt.Errorf(
			"%w: failed to parse manifest URL: %w",
			errFailedBuildManifestURL,
			err,
		)
	}

	originalHost := parsedURL.Host

	// Special handling for lscr.io registry redirects:
	// lscr.io (LinuxServer.io) images are hosted on GitHub Container Registry (ghcr.io)
	// but the registry redirects manifest requests from lscr.io to ghcr.io.
	// However, the authentication challenge comes from ghcr.io, and when we try to
	// make manifest requests to lscr.io, we get 401 Unauthorized followed by 404 Not Found
	// because lscr.io doesn't actually host the manifests - it's just a redirect endpoint.
	//
	// To fix this, we intercept lscr.io URLs and change the host to ghcr.io for manifest requests,
	// while still using lscr.io for the initial authentication challenge to get the correct tokens.
	// This ensures HEAD requests succeed with 200 OK and we can extract digests without
	// falling back to expensive full image pulls.
	//
	// The authentication flow works as follows:
	// 1. Initial challenge request to lscr.io/v2/ gets redirected to ghcr.io
	// 2. Authentication tokens are obtained from ghcr.io using the redirected challenge
	// 3. Manifest requests are made directly to ghcr.io (not lscr.io) to avoid 401/404 errors
	// 4. Digest extraction succeeds from the 200 OK response
	if parsedURL.Host == "lscr.io" {
		parsedURL.Host = "ghcr.io"
		manifestURL = parsedURL.String()
	}

	if parsedURL.Host == "" {
		return "", "", nil, fmt.Errorf(
			"%w: manifest URL has no host: %s",
			errFailedBuildManifestURL,
			manifestURL,
		)
	}

	if hostOverride != "" {
		parsedURL.Host = hostOverride
		manifestURL = parsedURL.String()
	}

	return manifestURL, originalHost, parsedURL, nil
}

// makeManifestRequest creates an HTTP request for fetching the manifest with proper headers and authentication.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - method: HTTP method ("HEAD" or "GET").
//   - manifestURL: The URL to request the manifest from.
//   - token: Authentication token (empty if not required).
//
// Returns:
//   - *http.Request: The constructed HTTP request.
//   - error: Non-nil if request creation fails, nil on success.
func makeManifestRequest(
	ctx context.Context,
	method, manifestURL, token string,
) (*http.Request, error) {
	// Construct the HTTP request with the appropriate method, headers, and context.
	req, err := http.NewRequestWithContext(ctx, method, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedCreateRequest, err)
	}

	// Set headers only if a token is provided.
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	// Set Accept header for Docker Distribution API manifest requests, supporting v1, v2, OCI v1, and OCI index.
	req.Header.Set(
		"Accept",
		"application/vnd.docker.distribution.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json",
	)
	req.Header.Set("User-Agent", UserAgent)

	return req, nil
}

// handleManifestResponse processes the HTTP response, handles redirects, and extracts the digest.
//
// It checks for redirects, updates the manifest URL if necessary, and extracts the digest
// from the response headers or body based on the request method.
//
// Parameters:
//   - resp: The HTTP response from the manifest request.
//   - method: The HTTP method used ("HEAD" or "GET").
//   - originalHost: The original host from the initial manifest URL.
//   - challengeHost: The challenge host from authentication (empty if not redirected).
//   - redirected: Whether authentication was redirected.
//   - parsedURL: Parsed URL for updating host.
//   - currentHost: The current host being used for the request.
//
// Returns:
//   - string: The extracted and normalized digest.
//   - string: Updated manifest URL if redirected, otherwise empty.
//   - bool: Whether a retry is needed.
//   - error: Non-nil if processing or extraction fails, nil on success.
func handleManifestResponse(
	resp *http.Response,
	method, originalHost, challengeHost string,
	redirected bool,
	parsedURL *url.URL,
	currentHost string,
) (string, string, bool, error) {
	fields := logrus.Fields{
		"method":         method,
		"status_code":    resp.StatusCode,
		"status":         resp.Status,
		"original_host":  originalHost,
		"challenge_host": challengeHost,
		"redirected":     redirected,
		"request_host":   resp.Request.URL.Host,
		"current_host":   currentHost,
	}

	logrus.WithFields(fields).Debug("Handling manifest response")

	var manifestURL string

	// Handle non-success responses for HEAD requests by returning empty digest to trigger GET fallback.
	// Exclude 404 Not Found to avoid unnecessary GET requests when the manifest doesn't exist.
	headFallbackCondition := method == http.MethodHead &&
		(resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices) &&
		resp.StatusCode != http.StatusNotFound
	logrus.WithFields(fields).
		WithField("head_fallback_condition", headFallbackCondition).
		Debug("Checking HEAD fallback condition")

	if headFallbackCondition {
		// For non-redirected registries, try challenge host first before falling back to GET
		if !redirected && challengeHost != "" && currentHost == originalHost &&
			(resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized) {
			logrus.WithFields(fields).
				WithField("retry_on_challenge", true).
				Debug("HEAD request failed on original host for non-redirected registry, trying challenge host")

			parsedURL.Host = challengeHost
			manifestURL = parsedURL.String()

			return "", manifestURL, true, nil
		}

		logrus.WithFields(fields).
			Debug("HEAD request failed, returning empty digest to trigger GET fallback")

		return "", "", false, nil // Return empty to trigger GET fallback in CompareDigest
	}

	// Check for redirect status codes (3xx)
	redirectCondition := resp.StatusCode >= http.StatusMultipleChoices &&
		resp.StatusCode < http.StatusBadRequest
	logrus.WithFields(fields).
		WithField("redirect_condition", redirectCondition).
		Debug("Checking redirect condition")

	if redirectCondition {
		// Handle manifest request redirects by updating URL to redirected host
		location := resp.Header.Get("Location")
		if location != "" {
			redirectURL, err := url.Parse(location)
			if err == nil && redirectURL.Host != "" && redirectURL.Host != currentHost {
				logrus.WithFields(fields).
					WithField("redirect_location", location).
					WithField("redirect_host", redirectURL.Host).
					Debug("Manifest request redirected, updating URL host")

				parsedURL.Host = redirectURL.Host
				manifestURL = parsedURL.String()

				return "", manifestURL, true, nil
			}
		}
	}

	// Check for successful status code (only for GET requests, since HEAD is handled above).
	successCondition := resp.StatusCode >= http.StatusOK &&
		resp.StatusCode < http.StatusMultipleChoices
	logrus.WithFields(fields).
		WithField("success_condition", successCondition).
		Debug("Checking success status condition")

	if !successCondition {
		// For HEAD requests, do not retry on 404 to avoid unnecessary GET fallback
		if method == http.MethodHead && resp.StatusCode == http.StatusNotFound {
			logrus.WithFields(fields).
				WithField("error", "HEAD request returned 404, not retrying").
				Debug("Response status not successful")

			return "", "", false, fmt.Errorf(
				"%w: status %s",
				errInvalidRegistryResponse,
				resp.Status,
			)
		}

		// Handle 401/404 errors on redirected hosts by retrying on original host
		if (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized) &&
			currentHost != originalHost {
			logrus.WithFields(fields).
				WithField("retry_on_original", true).
				Debug("401/404 on redirected host, retrying on original host")

			parsedURL.Host = originalHost
			manifestURL = parsedURL.String()

			return "", manifestURL, true, nil
		}

		// If we're on original host and have challenge host, try challenge host
		if (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized) &&
			challengeHost != "" && currentHost == originalHost {
			logrus.WithFields(fields).
				WithField("retry_on_challenge", true).
				Debug("401/404 on original host, trying challenge host")

			parsedURL.Host = challengeHost
			manifestURL = parsedURL.String()

			return "", manifestURL, true, nil
		}

		logrus.WithFields(fields).
			WithField("error", "invalid status code").
			Debug("Response status not successful")

		return "", "", false, fmt.Errorf("%w: status %s", errInvalidRegistryResponse, resp.Status)
	}

	// Extract the digest based on the request method (HEAD from headers, GET from body).
	var (
		digest string
		err    error
	)

	logrus.WithFields(fields).Debug("Extracting digest")

	if method == http.MethodHead {
		digest, err = extractHeadDigest(resp)
	} else {
		digest, err = ExtractGetDigest(resp)
	}

	if err != nil {
		logrus.WithFields(fields).WithError(err).Debug("Failed to extract digest")

		return "", "", false, err
	}

	logrus.WithFields(fields).
		WithField("extracted_digest", digest).
		Debug("Successfully extracted digest")

	return digest, "", false, nil
}

// retryManifestRequest performs a retry request to the manifest URL with updated host.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - method: HTTP method ("HEAD" or "GET").
//   - manifestURL: The updated manifest URL to retry.
//   - token: Authentication token.
//   - client: The authentication client to use for the request.
//
// Returns:
//   - *http.Response: The response from the retry request.
//   - error: Non-nil if the retry request fails, nil on success.

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
	registryAuth = auth.TransformAuth(registryAuth)

	// Create an authentication client for registry requests.
	client := auth.NewAuthClient()

	// Build initial manifest URL to get original host
	_, originalHost, _, err := buildManifestURL(container, "")
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to build manifest URL")

		return "", err
	}

	logrus.WithFields(fields).
		WithField("original_host", originalHost).
		Debug("Extracted original host from manifest URL")

	// Obtain an authentication token and challenge host for the registry.
	token, challengeHost, redirected, err := auth.GetToken(ctx, container, registryAuth, client)
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
			WithField("redirected", redirected).
			Debug("Received challenge host and redirect flag from GetToken")
	}

	// Build the manifest URL, using challenge host when redirected
	var (
		manifestURL string
		parsedURL   *url.URL
	)

	if challengeHost != "" && challengeHost != originalHost && redirected {
		manifestURL, _, parsedURL, err = buildManifestURL(container, challengeHost)
	} else {
		manifestURL, _, parsedURL, err = buildManifestURL(container, "")
	}

	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to build manifest URL")

		return "", err
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"method": method,
		"url":    manifestURL,
	}).Debug("Fetching digest")

	// Create the HTTP request for the manifest.
	req, err := makeManifestRequest(ctx, method, manifestURL, token)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithFields(logrus.Fields{
			"method": method,
			"url":    manifestURL,
		}).Debug("Failed to create request")

		return "", err
	}

	// Execute the initial request.
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithFields(logrus.Fields{
			"method": method,
			"url":    manifestURL,
		}).Debug("Failed to execute request")

		return "", fmt.Errorf("%w: %w", errFailedExecuteRequest, err)
	}
	defer resp.Body.Close()

	// Handle the manifest response, checking for redirects and extracting digest.
	digest, _, _, err := handleManifestResponse(
		resp,
		method,
		originalHost,
		challengeHost,
		redirected,
		parsedURL,
		parsedURL.Host,
	)
	if err != nil {
		logrus.WithError(err).WithFields(fields).WithField("status", resp.Status).
			Debug("Failed to handle manifest response")

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
	normalizedDigest := NormalizeDigest(digest)
	logrus.WithFields(logrus.Fields{
		"digest": normalizedDigest,
	}).Debug("Extracted digest from HEAD response")

	return normalizedDigest, nil
}

// ExtractGetDigest extracts the image digest from a GET response’s headers or body.
//
// It first tries to retrieve the digest from the "Docker-Content-Digest" header.
// If the header is missing, it falls back to parsing the response body as a JSON
// manifest or as a plain text digest for non-standard registries.
// When attempting JSON parsing, the Content-Type header must contain "application/json",
// "application/vnd.oci", or "application/vnd.docker".
// The digest is normalized for consistency.
//
// Parameters:
//   - resp: The HTTP response from a GET request containing headers and body.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if present.
//   - error: An error if the digest cannot be extracted, nil if successful.
func ExtractGetDigest(resp *http.Response) (string, error) {
	// First, try to retrieve the digest from the standard header.
	digest := resp.Header.Get(ContentDigestHeader)
	if digest != "" {
		// Normalize the digest (e.g., strip "sha256:") for consistency.
		normalizedDigest := NormalizeDigest(digest)
		logrus.WithFields(logrus.Fields{
			"digest": normalizedDigest,
		}).Debug("Extracted digest from GET response header")

		return normalizedDigest, nil
	}

	// Fallback: Try to read the response body.
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"status": resp.Status,
		}).Debug("Failed to read response body for digest extraction")

		return "", fmt.Errorf(
			"%w: failed to read response body: %w",
			errInvalidRegistryResponse,
			err,
		)
	}

	bodyStr := strings.TrimSpace(string(bodyBytes))
	if bodyStr == "" {
		logrus.WithFields(logrus.Fields{
			"status": resp.Status,
		}).Debug("Response body is empty, no digest found")

		return "", fmt.Errorf(
			"%w: missing digest header and empty body",
			errInvalidRegistryResponse,
		)
	}

	// Check if the response body starts with JSON indicators ('{' or '[') before attempting JSON unmarshaling.
	if strings.HasPrefix(bodyStr, "{") || strings.HasPrefix(bodyStr, "[") {
		// Check Content-Type for JSON parsing.
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "application/json") &&
			!strings.Contains(contentType, "application/vnd.oci") &&
			!strings.Contains(contentType, "application/vnd.docker") {
			return "", fmt.Errorf(
				"%w: unsupported content type for JSON parsing: %s",
				errInvalidRegistryResponse,
				contentType,
			)
		}
		// Try to parse as JSON manifest first (standard OCI/Docker format).
		// Define a struct to hold the expected JSON structure with a digest field.
		var manifest struct {
			Digest string `json:"digest"`
		}
		// Attempt to unmarshal the response body as JSON.
		var jsonErr error
		if jsonErr = json.Unmarshal(bodyBytes, &manifest); jsonErr == nil {
			// JSON unmarshaling succeeded, check if digest field contains a value.
			if manifest.Digest != "" {
				// Successfully parsed JSON manifest with digest field populated.
				normalizedDigest := NormalizeDigest(manifest.Digest)
				logrus.WithFields(logrus.Fields{
					"digest": normalizedDigest,
				}).Debug("Extracted digest from JSON manifest")

				return normalizedDigest, nil
			}
			// JSON parsed successfully but digest field is empty or missing.
			logrus.WithFields(logrus.Fields{
				"status": resp.Status,
				"body":   bodyStr,
			}).Debug("JSON manifest parsed but digest field is empty")

			return "", fmt.Errorf("%w: empty digest in JSON manifest", errInvalidRegistryResponse)
		}
		// JSON parsing failed, log metadata for debugging (avoid exposing potentially sensitive content).
		logrus.WithError(jsonErr).WithFields(logrus.Fields{
			"status":       resp.Status,
			"body_length":  len(bodyStr),
			"content_type": resp.Header.Get("Content-Type"),
		}).Debug("Failed to parse response body as JSON manifest")
	}

	// Final fallback: Try to parse as plain text digest for non-standard registries.
	// Validate that the body looks like a digest (starts with sha256: prefix and has reasonable length).
	if !strings.HasPrefix(bodyStr, "sha256:") || len(bodyStr) < 20 {
		logrus.WithFields(logrus.Fields{
			"status":       resp.Status,
			"body_length":  len(bodyStr),
			"content_type": resp.Header.Get("Content-Type"),
			"body":         bodyStr,
		}).Debug("Response body does not appear to be a valid digest")

		return "", fmt.Errorf("%w: invalid digest format in body", errInvalidRegistryResponse)
	}

	// Normalize the digest from the plain text body.
	normalizedDigest := NormalizeDigest(bodyStr)
	logrus.WithFields(logrus.Fields{
		"digest": normalizedDigest,
	}).Debug("Extracted digest from plain text body")

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
	normalizedRemoteDigest := NormalizeDigest(remoteDigest)

	for _, digest := range localDigests {
		// Split digest into repo and hash parts (e.g., "repo@sha256:abc").
		parts := strings.Split(digest, "@")
		if len(parts) < minDigestParts {
			continue // Skip malformed digests.
		}

		// Normalize the local digest’s hash part.
		normalizedLocalDigest := NormalizeDigest(parts[1])
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
