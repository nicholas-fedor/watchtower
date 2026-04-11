// Package digest provides functionality for retrieving and comparing Docker image digests in Watchtower.
// It enables the update process by fetching digests from container registries using HTTP requests,
// comparing them with local image digests, and handling authentication transformations to ensure compatibility
// with various registry authentication schemes. This package is integral to determining whether a container's
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
	"sync"

	"github.com/distribution/reference"
	dockerSystem "github.com/docker/docker/api/types/system"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ContentDigestHeader is the HTTP header key used to retrieve the digest from a registry's response.
// This header, typically "Docker-Content-Digest", contains the digest value (e.g., "sha256:abc...") for an image manifest,
// allowing Watchtower to compare or fetch it without downloading the full manifest body.
const ContentDigestHeader = "Docker-Content-Digest"

// imageLockEntry holds a per-image mutex along with a reference count and dead flag.
// When refs drops to zero, the entry is marked dead so new callers will revive it
// rather than creating a duplicate. This keeps the map bounded in practice while
// avoiding races between deletion and concurrent revival.
type imageLockEntry struct {
	mu      sync.Mutex
	inspect sync.Mutex
	refs    int
	dead    bool
}

// imageInspectLocks provides per-image-name mutex synchronization for ImageInspectWithRaw operations.
// Keys are canonicalized image references (e.g., "docker.io/library/nginx:latest") so that
// different representations of the same image share a single lock. Values are *imageLockEntry.
var imageInspectLocks sync.Map

type registryInfoProvider interface {
	Info(ctx context.Context) (dockerSystem.Info, error)
}

// Errors for digest retrieval operations.
var (
	// errMissingImageInfo indicates the container lacks image metadata, preventing digest operations.
	errMissingImageInfo = errors.New("container image info missing")
	// errInvalidRegistryResponse indicates the registry's HEAD response lacks a digest or is malformed.
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

// canonicalizeImageName resolves an image name to its fully-qualified canonical form
// using the distribution/reference library. This ensures that different representations
// of the same image (e.g., "nginx:latest" vs "docker.io/library/nginx:latest") resolve
// to the same lock key. Tags and digests are preserved so that different references
// to the same repository (e.g., repo:stable vs repo:canary) are serialized independently.
//
// Parameters:
//   - imageName: Raw image name from container configuration.
//
// Returns:
//   - string: Canonical image reference with tag/digest preserved, or the original name if parsing fails.
func canonicalizeImageName(imageName string) string {
	ref, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return imageName
	}

	return ref.String()
}

// getImageInspectLock retrieves or creates a reference-counted lock entry for the
// given image name. The image name is canonicalized so that different representations
// of the same image share a single mutex. The returned cleanup function must be called
// after the caller is finished with the lock to decrement the reference count and
// clean up the entry when no longer in use.
//
// The returned closure captures the exact *imageLockEntry pointer so that
// releaseImageInspectLockEntry always decrements the correct entry even if a
// concurrent caller has replaced the map entry in the interim.
//
// Parameters:
//   - imageName: Raw image name (may be short or fully-qualified).
//
// Returns:
//   - *sync.Mutex: The per-image mutex for serializing inspections.
//   - func(): Cleanup function that decrements the reference count.
func getImageInspectLock(imageName string) (*sync.Mutex, func()) {
	key := canonicalizeImageName(imageName)

	// Create a candidate entry optimistically. If another goroutine wins the
	// LoadOrStore race we discard this one and use theirs instead.
	newEntry := &imageLockEntry{refs: 1}

	val, loaded := imageInspectLocks.LoadOrStore(key, newEntry)
	if loaded {
		// Another goroutine already stored an entry; discard ours and
		// revive or increment the existing one.
		existing, isEntry := val.(*imageLockEntry)
		if isEntry {
			existing.mu.Lock()

			// Check dead before incrementing: if the entry was marked dead by a
			// concurrent release that is about to remove it from the map, do not
			// revive it — let it be deleted and create a fresh entry instead.
			if existing.dead {
				existing.mu.Unlock()

				fresh := &imageLockEntry{refs: 1}
				imageInspectLocks.Store(key, fresh)

				return &fresh.inspect, func() { releaseImageInspectLockEntry(key, fresh) }
			}

			existing.refs++
			existing.mu.Unlock()

			return &existing.inspect, func() { releaseImageInspectLockEntry(key, existing) }
		}

		// Unexpected type in map; overwrite with our entry.
		imageInspectLocks.Store(key, newEntry)
	}

	return &newEntry.inspect, func() { releaseImageInspectLockEntry(key, newEntry) }
}

// releaseImageInspectLockEntry decrements the reference count for the given lock
// entry. When the count reaches zero the entry is conditionally removed from the
// map (only if it is still the same entry) to avoid races with concurrent
// getImageInspectLock calls that may have replaced it.
//
// Unlike releaseImageInspectLock (which loads the entry from the map by key),
// this function operates on the exact pointer captured at acquisition time,
// preventing the revive/delete race where a stale key-based lookup could
// decrement the wrong entry's ref count.
//
// Parameters:
//   - key: Canonical image reference key used for conditional map deletion.
//   - entry: The specific lock entry to release.
func releaseImageInspectLockEntry(key string, entry *imageLockEntry) {
	entry.mu.Lock()
	entry.refs--

	if entry.refs == 0 {
		entry.dead = true

		// Hold entry.mu while removing from the map so a concurrent
		// getImageInspectLock cannot revive the entry between the dead
		// flag being set and the map entry being deleted.
		imageInspectLocks.CompareAndDelete(key, entry)
		entry.mu.Unlock()

		return
	}

	entry.mu.Unlock()
}

// releaseImageInspectLock decrements the reference count for the lock entry
// associated with the given canonical image key. This is the key-based variant;
// prefer releaseImageInspectLockEntry when the exact entry pointer is available.
//
// Parameters:
//   - key: Canonical image reference key.
func releaseImageInspectLock(key string) {
	val, loaded := imageInspectLocks.Load(key)
	if !loaded {
		return
	}

	entry, isEntry := val.(*imageLockEntry)
	if !isEntry {
		return
	}

	releaseImageInspectLockEntry(key, entry)
}

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
		if after, ok := strings.CutPrefix(digest, prefix); ok {
			// Trim the prefix to get the raw digest value.
			normalized := after
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

// CompareDigest checks whether a container's current image digest matches the latest from its registry.
//
// It first inspects the image to check if it's locally built (empty RepoDigests).
// For local images, digest comparison against a remote registry is not possible,
// so it returns true to indicate the image should not be updated. This avoids
// unnecessary HTTP requests and confusing error messages for locally built images.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - inspector: Image inspector for checking if image is locally built.
//   - container: Container whose digest is being compared.
//   - registryAuth: Base64-encoded auth string.
//
// Returns:
//   - bool: True if digests match (image is up-to-date), false otherwise.
//   - error: Non-nil if operation fails, nil on success.
func CompareDigest(
	ctx context.Context,
	inspector types.ImageInspector,
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

	// Check if the container's image has no RepoDigests, which indicates a locally
	// built image that has never been pushed or pulled. For such images, there is
	// no remote digest to compare against, so we treat them as up-to-date to avoid
	// unnecessary registry requests and confusing error messages.
	//
	// We check container.ImageInfo().RepoDigests rather than inspecting via the
	// Docker daemon because:
	// 1. The container was already populated with image info during initialization
	// 2. For locally built images, RepoDigests is always empty
	// 3. This avoids an extra Docker daemon call
	if len(container.ImageInfo().RepoDigests) == 0 {
		logrus.WithFields(fields).
			Debug("Image with no registry reference detected (empty RepoDigests) - skipping digest comparison")

		return true, nil
	}

	// Fetch the latest digest from the registry using a HEAD request for efficiency.
	remoteDigest, err := fetchDigest(
		ctx,
		inspector,
		container,
		registryAuth,
		http.MethodHead,
	)
	if err != nil {
		return false, err
	}

	// If HEAD request returned empty digest (due to missing Docker-Content-Digest header),
	// fall back to GET request.
	if remoteDigest == "" {
		logrus.WithFields(fields).
			Debug("HEAD request returned empty digest - falling back to GET")

		remoteDigest, err = FetchDigest(
			ctx,
			inspector,
			container,
			registryAuth,
		)
		if err != nil {
			return false, err
		}
	}

	// Compare the fetched remote digest with the container's local digests.
	matches := DigestsMatch(
		container.ImageInfo().RepoDigests,
		remoteDigest,
	)
	logrus.WithFields(fields).
		WithField("matches", matches).
		Debug("Completed digest comparison")

	return matches, nil
}

// FetchDigest retrieves the digest of an image from its registry using a GET request.
// It fetches the manifest to ensure the digest header is present, which may be necessary when
// HEAD requests are unsupported. The digest is extracted from the response headers and normalized for consistency.
//
// Parameters:
//   - ctx: The context controlling the request's lifecycle, enabling cancellation or timeouts.
//   - inspector: Image inspector for checking if image is locally built.
//   - container: The container whose image digest is being fetched, providing the image name and reference.
//   - authToken: A base64-encoded authentication string for registry access.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if successful.
//   - error: An error if the request fails or digest header is missing, nil if successful.
func FetchDigest(
	ctx context.Context,
	inspector types.ImageInspector,
	container types.Container,
	authToken string,
) (string, error) {
	return fetchDigest(ctx, inspector, container, authToken, http.MethodGet)
}

// BuildManifestURL constructs and validates the manifest URL for a container.
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
func BuildManifestURL(
	container types.Container,
	hostOverride string,
) (string, string, *url.URL, error) {
	return BuildManifestURLWithRegistryEndpoint(container, hostOverride, "")
}

// BuildManifestURLWithRegistryEndpoint constructs a manifest URL using either the
// image's canonical registry or an explicit registry endpoint override such as a
// Docker registry mirror.
func BuildManifestURLWithRegistryEndpoint(
	container types.Container,
	hostOverride string,
	registryEndpoint string,
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

	if registryEndpoint != "" {
		endpointURL, err := buildRegistryEndpointURL(registryEndpoint, parsedURL.Path)
		if err != nil {
			return "", "", nil, fmt.Errorf(
				"%w: invalid registry endpoint %q: %w",
				errFailedBuildManifestURL,
				registryEndpoint,
				err,
			)
		}

		originalHost = endpointURL.Host
		parsedURL = endpointURL
		manifestURL = parsedURL.String()
	}

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

// fetchDigest retrieves an image digest using the specified HTTP method.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - inspector: Image inspector for checking if image is locally built.
//   - container: Container whose digest is being retrieved.
//   - registryAuth: Base64-encoded auth string.
//   - method: HTTP method ("HEAD" or "GET").
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil if operation fails, nil on success.
func fetchDigest(
	ctx context.Context,
	inspector types.ImageInspector,
	container types.Container,
	registryAuth string,
	method string,
) (string, error) {
	endpoints := getRegistryEndpoints(ctx, inspector, container)
	var lastErr error

	for _, endpoint := range endpoints {
		digest, err := fetchDigestFromRegistryEndpoint(
			ctx,
			inspector,
			container,
			registryAuth,
			method,
			endpoint,
		)
		if err == nil {
			return digest, nil
		}

		lastErr = err
		logrus.WithError(err).WithFields(logrus.Fields{
			"container":         container.Name(),
			"image":             container.ImageName(),
			"method":            method,
			"registry_endpoint": endpoint,
		}).Debug("Digest fetch failed for registry endpoint")
	}

	return "", lastErr
}

func fetchDigestFromRegistryEndpoint(
	ctx context.Context,
	inspector types.ImageInspector,
	container types.Container,
	registryAuth string,
	method string,
	registryEndpoint string,
) (string, error) {
	fields := logrus.Fields{
		"container":         container.Name(),
		"image":             container.ImageName(),
		"registry_endpoint": registryEndpoint,
	}

	// Inspect the image to check if it's locally built (no RepoDigests).
	// Use a per-image-name mutex to serialize inspections of the same image
	// while allowing concurrent inspections of different images. This prevents
	// redundant concurrent requests to the Docker daemon for the same image
	// without blocking unrelated image inspections.
	//
	// The lock is scoped to only the daemon call; network I/O below runs
	// concurrently for different images.
	imageName := container.ImageName()

	lock, release := getImageInspectLock(imageName)

	lock.Lock()

	inspect, _, err := inspector.ImageInspectWithRaw(ctx, imageName)

	lock.Unlock()
	release()

	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to inspect image")

		return "", fmt.Errorf("%w: %w", errFailedExecuteRequest, err)
	}

	// Skip digest fetching for locally built images (empty RepoDigests).
	if len(inspect.RepoDigests) == 0 {
		logrus.WithFields(fields).Debug("Skipping digest fetch for locally built image")

		return "", nil
	}

	// Transform the provided auth string into a usable format for registry authentication.
	registryAuth = auth.TransformAuth(registryAuth)

	// Create an authentication client for registry requests.
	client := auth.NewAuthClient()

	// Build initial manifest URL to get original host
	_, originalHost, _, err := BuildManifestURLWithRegistryEndpoint(container, "", registryEndpoint)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to build manifest URL")

		return "", err
	}

	logrus.WithFields(fields).
		WithField("original_host", originalHost).
		Debug("Extracted original host from manifest URL")

	// Obtain an authentication token and challenge host for the registry.
	token, challengeHost, redirected, redirectHost, err := auth.GetTokenWithRegistryEndpoint(
		ctx,
		container,
		registryAuth,
		client,
		registryEndpoint,
	)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get token")

		return "", fmt.Errorf("%w: %w", errFailedGetToken, err)
	}

	// If no token is returned, authentication is not required.
	if token == "" {
		logrus.WithFields(fields).
			Debug("No authentication required, proceeding with request")
	} else {
		logrus.WithFields(fields).
			WithField("challenge_host", challengeHost).
			WithField("redirected", redirected).
			WithField("redirect_host", redirectHost).
			Debug("Received challenge host and redirect flag from GetToken")
	}

	// Build the manifest URL, using redirect host when redirected
	var (
		manifestURL string
		parsedURL   *url.URL
	)

	if redirectHost != "" && redirectHost != originalHost && redirected {
		manifestURL, _, parsedURL, err = BuildManifestURLWithRegistryEndpoint(
			container,
			redirectHost,
			registryEndpoint,
		)
	} else {
		manifestURL, _, parsedURL, err = BuildManifestURLWithRegistryEndpoint(
			container,
			"",
			registryEndpoint,
		)
	}

	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to build manifest URL")

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

	defer func() { _ = resp.Body.Close() }()

	// Handle the manifest response, checking for redirects and extracting digest.
	digest, updatedURL, retry, err := HandleManifestResponse(
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

	if retry && updatedURL != "" {
		logrus.WithFields(fields).
			WithField("retry_url", updatedURL).
			Debug("Retrying manifest request with updated URL")

		digest, err = retryManifestRequest(
			ctx,
			method,
			updatedURL,
			token,
			originalHost,
			challengeHost,
			redirected,
			parsedURL,
			client,
		)
		if err != nil {
			logrus.WithError(err).WithFields(fields).WithField("retry_url", updatedURL).
				Debug("Failed to retry manifest request")

			return "", err
		}
	}

	logrus.WithFields(fields).WithField("remote_digest", digest).
		Debug("Fetched remote digest")

	return digest, nil
}

func getRegistryEndpoints(
	ctx context.Context,
	inspector types.ImageInspector,
	container types.Container,
) []string {
	registryHost, err := auth.GetRegistryAddress(container.ImageName())
	if err != nil || registryHost != auth.DockerRegistryHost {
		return []string{""}
	}

	infoProvider, ok := inspector.(registryInfoProvider)
	if !ok {
		return []string{""}
	}

	info, err := infoProvider.Info(ctx)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"container": container.Name(),
			"image":     container.ImageName(),
		}).Debug("Failed to read registry mirrors from daemon info")

		return []string{""}
	}

	if info.RegistryConfig == nil || len(info.RegistryConfig.Mirrors) == 0 {
		return []string{""}
	}

	seen := map[string]struct{}{}
	endpoints := make([]string, 0, len(info.RegistryConfig.Mirrors)+1)

	for _, mirror := range info.RegistryConfig.Mirrors {
		mirror = strings.TrimSpace(mirror)
		if mirror == "" {
			continue
		}

		if _, exists := seen[mirror]; exists {
			continue
		}

		seen[mirror] = struct{}{}
		endpoints = append(endpoints, mirror)
	}

	return append(endpoints, "")
}

func buildRegistryEndpointURL(registryEndpoint, resourcePath string) (*url.URL, error) {
	scheme := "https"
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		scheme = "http"
	}

	endpointURL, err := url.Parse(registryEndpoint)
	if err != nil {
		return nil, err
	}

	if endpointURL.Scheme == "" && endpointURL.Host == "" && endpointURL.Path != "" {
		endpointURL, err = url.Parse(scheme + "://" + registryEndpoint)
		if err != nil {
			return nil, err
		}
	}

	if endpointURL.Scheme == "" {
		endpointURL.Scheme = scheme
	}

	if endpointURL.Host == "" {
		return nil, fmt.Errorf("missing host")
	}

	endpointURL.Path = joinURLPath(endpointURL.Path, resourcePath)
	endpointURL.RawPath = ""
	endpointURL.RawQuery = ""
	endpointURL.Fragment = ""

	return endpointURL, nil
}

func joinURLPath(basePath, resourcePath string) string {
	switch {
	case basePath == "":
		return resourcePath
	case strings.HasSuffix(basePath, "/") && strings.HasPrefix(resourcePath, "/"):
		return basePath + strings.TrimPrefix(resourcePath, "/")
	case !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(resourcePath, "/"):
		return basePath + "/" + resourcePath
	default:
		return basePath + resourcePath
	}
}

// HandleManifestResponse processes the HTTP response, handles redirects, and extracts the digest.
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
func HandleManifestResponse(
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

			logrus.WithFields(fields).
				WithField("retry_url", manifestURL).
				Debug("Setting retry due to HEAD failure on original host")

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

				logrus.WithFields(fields).
					WithField("retry_url", manifestURL).
					Debug("Setting retry due to redirect")

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

			logrus.WithFields(fields).
				WithField("retry_url", manifestURL).
				Debug("Setting retry due to 401/404 on redirected host")

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

			logrus.WithFields(fields).
				WithField("retry_url", manifestURL).
				Debug("Setting retry due to 401/404 on original host with challenge host")

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
		digest, err = ExtractHeadDigest(resp)
	} else {
		digest, err = ExtractGetDigest(resp)
	}

	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to extract digest")

		return "", "", false, err
	}

	logrus.WithFields(fields).
		WithField("extracted_digest", digest).
		Debug("Successfully extracted digest")

	return digest, "", false, nil
}

// ExtractHeadDigest extracts the image digest from a HEAD response's headers.
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
func ExtractHeadDigest(resp *http.Response) (string, error) {
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

// ExtractGetDigest extracts the image digest from a GET response's headers or body.
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
		jsonErr := json.Unmarshal(bodyBytes, &manifest)
		if jsonErr == nil {
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

// DigestsMatch compares a list of local digests with a remote digest to determine if there's a match.
//
// It normalizes both the remote digest and each local digest, checking for equality to confirm
// whether the container's image is up-to-date with the registry's latest version.
//
// Parameters:
//   - localDigests: A slice of local digests from the container's image info (e.g., "sha256:abc...").
//   - remoteDigest: The digest fetched from the registry (e.g., "sha256:abc..." or raw hash).
//
// Returns:
//   - bool: True if any normalized local digest matches the normalized remote digest, false otherwise.
func DigestsMatch(localDigests []string, remoteDigest string) bool {
	// Normalize the remote digest once for efficiency.
	normalizedRemoteDigest := NormalizeDigest(remoteDigest)

	logrus.WithFields(logrus.Fields{
		"local_digests": localDigests,
		"remote_digest": normalizedRemoteDigest,
	}).Debug("Comparing digests")

	for _, digest := range localDigests {
		// Cut the digest into repo and hash parts (e.g., "repo@sha256:abc").
		repo, after, found := strings.Cut(digest, "@")

		// Skip malformed digests without @ separator.
		if !found {
			logrus.WithFields(logrus.Fields{
				"digest": digest,
			}).Debug("Skipping malformed digest without @ separator")

			continue
		}

		// Handle case where digest starts with "@" (e.g., "@sha256:abc123")
		// This is a valid format that Docker may report in some contexts.
		if repo == "" {
			logrus.WithFields(logrus.Fields{
				"digest":        digest,
				"remote_digest": normalizedRemoteDigest,
			}).Debug("Local digest has empty repo prefix, comparing only digest part")
		}

		// Remove the sha256: prefix, if needed.
		normalizedLocalDigest := NormalizeDigest(after)

		// Return true on the first match.
		if normalizedLocalDigest == normalizedRemoteDigest {
			logrus.WithFields(logrus.Fields{
				"local_digest":  digest,
				"remote_digest": normalizedRemoteDigest,
			}).Debug("Found digest match")

			return true
		}
	}

	return false
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
	req.Header.Set("User-Agent", meta.UserAgent)

	return req, nil
}

// retryManifestRequest performs a retry request to the manifest URL with updated host and returns the digest.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - method: HTTP method ("HEAD" or "GET").
//   - updatedURL: The updated manifest URL to retry.
//   - token: Authentication token.
//   - originalHost: The original host.
//   - challengeHost: The challenge host.
//   - redirected: Whether authentication was redirected.
//   - parsedURL: Parsed URL object.
//   - client: The HTTP client to use for the request.
//
// Returns:
//   - string: The extracted digest.
//   - error: Non-nil if the retry request fails, nil on success.
func retryManifestRequest(
	ctx context.Context,
	method, updatedURL, token string,
	originalHost, challengeHost string,
	redirected bool,
	parsedURL *url.URL,
	client auth.Client,
) (string, error) {
	req, err := makeManifestRequest(ctx, method, updatedURL, token)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFailedExecuteRequest, err)
	}

	defer func() { _ = resp.Body.Close() }()

	digest, _, _, err := HandleManifestResponse(
		resp,
		method,
		originalHost,
		challengeHost,
		redirected,
		parsedURL,
		parsedURL.Host,
	)
	if err != nil {
		return "", err
	}

	return digest, nil
}
