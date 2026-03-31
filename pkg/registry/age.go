package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for image age retrieval operations.
var (
	// errFetchManifestFailed indicates a failure to fetch the image manifest from the registry.
	errFetchManifestFailed = errors.New("failed to fetch manifest from registry")
	// errNoConfigDigest indicates the manifest does not contain a config digest.
	errNoConfigDigest = errors.New("manifest does not contain config digest")
	// errFetchConfigFailed indicates a failure to fetch the image config blob from the registry.
	errFetchConfigFailed = errors.New("failed to fetch config blob from registry")
	// errParseConfigFailed indicates a failure to parse the image config JSON.
	errParseConfigFailed = errors.New("failed to parse image config")
	// errImageCreationTimeMissing indicates the image config does not contain a creation timestamp.
	errImageCreationTimeMissing = errors.New("image config does not contain creation timestamp")
	// errNoPlatformMatch indicates no matching platform was found in the image index.
	errNoPlatformMatch = errors.New("no matching platform found in image index")
	// errAmbiguousPlatformMatch indicates multiple platform entries matched OS/Architecture with differing Variants.
	errAmbiguousPlatformMatch = errors.New("ambiguous platform match: multiple entries with differing variants")
	// errMissingTag indicates the image reference does not contain a tag.
	errMissingTag = errors.New("missing tag in image reference")
	// errManifestTooLarge indicates the manifest response exceeded the size limit.
	errManifestTooLarge = errors.New("manifest response exceeds size limit")
)

// Size limits for registry responses to prevent unbounded memory allocation.
const (
	// maxManifestSize is the maximum allowed size for a manifest response body (1 MiB).
	maxManifestSize = 1 << 20
	// maxConfigSize is the maximum allowed size for a config blob response body (4 MiB).
	maxConfigSize = 1 << 22
)

// imageIndex represents a multi-platform image index (OCI or Docker manifest list).
type imageIndex struct {
	MediaType     string       `json:"mediaType"`
	SchemaVersion int          `json:"schemaVersion"`
	Manifests     []indexEntry `json:"manifests"`
}

// indexEntry represents a single entry in an image index.
type indexEntry struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Platform    platform          `json:"platform"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// platform represents the platform of an image in an index.
type platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// imageManifest represents a single-platform image manifest (OCI or Docker v2).
type imageManifest struct {
	MediaType     string `json:"mediaType"`
	SchemaVersion int    `json:"schemaVersion"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	} `json:"config"`
}

// imageConfig represents the relevant fields from an image config blob.
type imageConfig struct {
	Created *time.Time `json:"created"`
}

// FetchImageCreationTime retrieves the image creation timestamp from the registry
// by fetching the manifest and config blob via the OCI Distribution Spec API.
// It handles multi-platform images by selecting the correct platform manifest
// and follows redirects for blob requests.
//
// For cross-platform monitoring, set WATCHTOWER_COOLDOWN_PLATFORM_OS and
// WATCHTOWER_COOLDOWN_PLATFORM_ARCH to override the runtime defaults.
// Set WATCHTOWER_COOLDOWN_PLATFORM_VARIANT to specify a platform variant
// (e.g., "v7", "v8") for ARM images with multiple variants.
//
// This function does NOT download the image layers — only the manifest and config
// blob (typically <15KB total) are fetched.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container whose image creation time to fetch.
//   - registryAuth: Base64-encoded registry credentials.
//
// Returns:
//   - time.Time: The image creation timestamp from the registry config.
//   - error: Non-nil if any step fails.
func FetchImageCreationTime(
	ctx context.Context,
	container types.Container,
	registryAuth string,
) (time.Time, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Determine target platform (supports cross-platform monitoring overrides).
	targetOS := viper.GetString("WATCHTOWER_COOLDOWN_PLATFORM_OS")
	targetArch := viper.GetString("WATCHTOWER_COOLDOWN_PLATFORM_ARCH")
	targetVariant := viper.GetString("WATCHTOWER_COOLDOWN_PLATFORM_VARIANT")

	// Transform auth credentials into usable format.
	registryAuth = auth.TransformAuth(registryAuth)

	// Use the cached HTTP client for registry requests.
	client := auth.NewAuthClient()

	// Obtain an authentication token and challenge host for the registry.
	token, challengeHost, redirected, redirectHost, err := auth.GetToken(
		ctx,
		container,
		registryAuth,
		client,
	)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to get auth token for image age check")

		return time.Time{},
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	// Build the initial manifest URL to get the original host.
	manifestURL, originalHost, parsedURL, err := buildManifestURLForAge(
		container,
		"",
	)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to build manifest URL")

		return time.Time{}, fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	// Determine the primary manifest host based on auth redirect.
	// If redirected during auth, use the redirect host for manifest requests.
	if redirectHost != "" && redirectHost != originalHost && redirected {
		manifestURL, _, parsedURL, err = buildManifestURLForAge(
			container,
			redirectHost,
		)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to build manifest URL with redirect host")

			return time.Time{},
				fmt.Errorf("%w: %w", errFetchManifestFailed, err)
		}
	}

	// Try manifest fetch with host fallback: primary (redirect/original),
	// then challenge host, then original host.
	configDigest, winningHost, err := fetchManifestForAge(
		ctx,
		client,
		manifestURL,
		token,
		parsedURL,
		targetOS,
		targetArch,
		targetVariant,
		challengeHost,
		originalHost,
		fields,
	)
	if err != nil {
		return time.Time{}, err
	}

	logrus.WithFields(fields).
		WithField("config_digest", configDigest).
		Debug("Extracted config digest from manifest")

	// Use the host that successfully served the manifest for the blob fetch.
	blobURL := *parsedURL
	blobURL.Host = winningHost

	// Fetch the config blob.
	configBody, err := fetchConfigBlob(
		ctx,
		client,
		&blobURL,
		configDigest,
		token,
		fields,
	)
	if err != nil {
		return time.Time{}, err
	}
	defer configBody.Close()

	// Parse the config JSON with a size limit to prevent unbounded memory allocation.
	var config imageConfig

	err = json.NewDecoder(io.LimitReader(configBody, maxConfigSize+1)).Decode(&config)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to parse image config JSON")

		return time.Time{},
			fmt.Errorf("%w: %w", errParseConfigFailed, err)
	}

	// Check if the created field is present.
	if config.Created == nil {
		logrus.WithFields(fields).
			Debug("Image config does not contain creation timestamp")

		return time.Time{}, errImageCreationTimeMissing
	}

	logrus.WithFields(fields).
		WithField("created", config.Created).
		Debug("Fetched image creation time from registry")

	return *config.Created, nil
}

// buildManifestURLForAge constructs the manifest URL for image age checking.
// It handles scheme selection based on TLS configuration and lscr.io host swapping.
//
// Parameters:
//   - container: Container whose image manifest URL to build.
//   - hostOverride: Optional host override (empty string to use original).
//
// Returns:
//   - string: The manifest URL.
//   - string: The original host.
//   - *url.URL: The parsed URL.
//   - error: Non-nil if URL construction fails.
func buildManifestURLForAge(
	container types.Container,
	hostOverride string,
) (string, string, *url.URL, error) {
	// Determine scheme based on TLS skip configuration.
	scheme := "https"
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		scheme = "http"
	}

	// Build manifest URL.
	manifestURLStr, err := buildManifestURLForContainer(container, scheme)
	if err != nil {
		return "",
			"",
			nil,
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	// Parse the URL.
	parsedURL, err := url.Parse(manifestURLStr)
	if err != nil {
		return "",
			"",
			nil,
			fmt.Errorf(
				"%w: failed to parse manifest URL: %w",
				errFetchManifestFailed,
				err,
			)
	}

	originalHost := parsedURL.Host

	// Handle lscr.io → ghcr.io host swap.
	if parsedURL.Host == "lscr.io" {
		parsedURL.Host = "ghcr.io"
		manifestURLStr = parsedURL.String()
	}

	if hostOverride != "" {
		parsedURL.Host = hostOverride
		manifestURLStr = parsedURL.String()
	}

	return manifestURLStr, originalHost, parsedURL, nil
}

// retryManifestRequest fetches manifest bytes from the registry with host-based retry.
//
// It iterates through candidate hosts (primary, challenge, original) and returns the
// raw response body on the first successful HTTP 200. On 401/404, it advances to the
// next host. The response body is size-limited to maxManifestSize.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - parsedURL: Parsed manifest URL used as the base for host substitution.
//   - manifestURL: Original manifest URL string (to avoid redundant re-fetch).
//   - token: Authentication token for the Authorization header.
//   - challengeHost: Registry challenge host for fallback.
//   - originalHost: The true original registry host before any redirects.
//   - fields: Logging fields for context.
//
// Returns:
//   - []byte: Raw manifest response body bytes.
//   - string: The winning host that served the manifest.
//   - string: The HTTP Content-Type header value from the response.
//   - error: Non-nil if all hosts fail or body exceeds size limit.
func retryManifestRequest(
	ctx context.Context,
	client auth.Client,
	parsedURL *url.URL,
	manifestURL, token, challengeHost, originalHost string,
	fields logrus.Fields,
) ([]byte, string, string, error) {
	// Use the provided originalHost for fallback; if empty, derive from parsedURL.
	if originalHost == "" {
		originalHost = parsedURL.Host
	}

	if fields["original_host"] == nil {
		fields["original_host"] = originalHost
	}

	// Build list of hosts to try: primary (current), then challenge, then original.
	// This mirrors the retry chain in digest.go's HandleManifestResponse.
	type hostCandidate struct {
		host string
		name string
	}

	currentHost := parsedURL.Host

	hosts := []hostCandidate{{host: currentHost, name: "primary"}}

	// Add challenge host if different from current and available.
	if challengeHost != "" && challengeHost != currentHost {
		hosts = append(hosts, hostCandidate{host: challengeHost, name: "challenge"})
	}

	// Add original host if different from current and challenge.
	if originalHost != currentHost && originalHost != challengeHost {
		hosts = append(hosts, hostCandidate{host: originalHost, name: "original"})
	}

	var lastErr error

	for i, candidate := range hosts {
		// Build URL for this host attempt.
		attemptURL := *parsedURL
		attemptURL.Host = candidate.host
		attemptManifestURL := attemptURL.String()

		// Skip re-fetching if this is the same URL as the initial manifestURL.
		if i > 0 || attemptManifestURL != manifestURL {
			logrus.WithFields(fields).
				WithField("attempt_host", candidate.host).
				WithField("attempt_name", candidate.name).
				Debug("Retrying manifest fetch on alternate host")
		}

		// Create the manifest request with broad Accept headers.
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			attemptManifestURL,
			nil,
		)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to create manifest request")

			return nil,
				"",
				"",
				fmt.Errorf(
					"%w: %w",
					errFetchManifestFailed,
					err,
				)
		}

		if token != "" {
			req.Header.Set("Authorization", token)
		}

		// Accept all supported manifest types.
		req.Header.Set("Accept", strings.Join([]string{
			"application/vnd.oci.image.index.v1+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.oci.image.manifest.v1+json",
			"application/vnd.docker.distribution.manifest.v2+json",
		}, ", "))
		req.Header.Set("User-Agent", meta.UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to execute manifest request")

			return nil,
				"",
				"",
				fmt.Errorf("%w: %w", errFetchManifestFailed, err)
		}

		if resp.StatusCode != http.StatusOK {
			status := resp.StatusCode
			resp.Body.Close()

			if (status == http.StatusUnauthorized || status == http.StatusNotFound) &&
				i < len(hosts)-1 {
				logrus.WithFields(fields).
					WithField("status", status).
					WithField("host", candidate.host).
					Debug("Manifest request failed, trying next host")

				lastErr = fmt.Errorf(
					"%w: status %d on %s",
					errFetchManifestFailed,
					status,
					candidate.host,
				)

				continue
			}

			logrus.WithFields(fields).
				WithField("status", status).
				Debug("Manifest request returned non-OK status")

			return nil,
				"",
				"",
				fmt.Errorf(
					"%w: status %d",
					errFetchManifestFailed,
					status,
				)
		}

		// Capture Content-Type header for media type fallback detection.
		contentType := resp.Header.Get("Content-Type")

		// Read the manifest body with a size limit to prevent unbounded memory allocation.
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
		resp.Body.Close()

		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to read manifest response")

			return nil,
				"",
				"",
				fmt.Errorf("%w: %w", errFetchManifestFailed, err)
		}

		if len(body) > maxManifestSize {
			logrus.WithFields(fields).
				WithField("size", len(body)).
				WithField("limit", maxManifestSize).
				Debug("Manifest response exceeds size limit")

			return nil,
				"",
				"",
				fmt.Errorf(
					"%w: %d bytes exceeds limit of %d bytes",
					errManifestTooLarge,
					len(body),
					maxManifestSize,
				)
		}

		return body, candidate.host, contentType, nil
	}

	// All hosts exhausted.
	if lastErr != nil {
		return nil, "", "", lastErr
	}

	return nil,
		"",
		"",
		fmt.Errorf("%w: no hosts to try", errFetchManifestFailed)
}

// fetchManifestForAge fetches the image manifest and extracts the config digest.
// It handles multi-platform indexes by selecting the platform-specific manifest.
//
// On 401/404 responses, it retries across alternate hosts (challenge host, original host)
// mirroring the retry logic in digest.go's HandleManifestResponse.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - manifestURL: URL of the manifest to fetch.
//   - token: Authentication token.
//   - parsedURL: Parsed manifest URL.
//   - targetOS: Target OS for platform selection (empty for runtime.GOOS).
//   - targetArch: Target architecture for platform selection (empty for runtime.GOARCH).
//   - targetVariant: Target variant for platform selection (empty for any variant).
//   - challengeHost: Registry challenge host for fallback.
//   - originalHost: The true original registry host before any redirects.
//   - fields: Logging fields for context.
//
// Returns:
//   - string: Config digest (e.g., "sha256:abc...").
//   - error: Non-nil if fetch, parse, or platform selection fails.
func fetchManifestForAge(
	ctx context.Context,
	client auth.Client,
	manifestURL, token string,
	parsedURL *url.URL,
	targetOS, targetArch, targetVariant, challengeHost, originalHost string,
	fields logrus.Fields,
) (string, string, error) {
	body, winningHost, contentType, err := retryManifestRequest(
		ctx,
		client,
		parsedURL,
		manifestURL,
		token,
		challengeHost,
		originalHost,
		fields,
	)
	if err != nil {
		return "", "", err
	}

	// Try to parse as image index (multi-platform).
	var index imageIndex

	idxErr := json.Unmarshal(body, &index)
	// Use mediaType from JSON body if present, otherwise fall back to HTTP Content-Type header.
	// Some registries (e.g., Amazon ECR) omit mediaType from the JSON body but set the
	// Content-Type header correctly.
	effectiveMediaType := index.MediaType
	if effectiveMediaType == "" {
		// Extract the media type from Content-Type, ignoring parameters like charset.
		if idx := strings.Index(contentType, ";"); idx != -1 {
			contentType = strings.TrimSpace(contentType[:idx])
		}

		effectiveMediaType = contentType
	}

	if idxErr == nil && isIndexMediaType(effectiveMediaType) {
		// For multi-platform images, selectPlatformManifest performs its own
		// host fallback internally. Build a parsedURL with the winning host so
		// the platform-specific fetch starts from the correct origin.
		effectiveURL := *parsedURL
		effectiveURL.Host = winningHost

		return selectPlatformManifest(
			ctx,
			client,
			index,
			&effectiveURL,
			token,
			targetOS,
			targetArch,
			targetVariant,
			challengeHost,
			fields,
		)
	}

	// Parse as single-platform manifest.
	var manifest imageManifest

	err = json.Unmarshal(body, &manifest)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to parse manifest JSON")

		return "",
			"",
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	if manifest.Config.Digest == "" {
		logrus.WithFields(fields).
			Debug("Manifest does not contain config digest")

		return "", "", errNoConfigDigest
	}

	return manifest.Config.Digest, winningHost, nil
}

// selectPlatformCandidate filters the image index for matching platform entries
// and disambiguates variant conflicts.
//
// It skips attestation manifests, collects entries matching the target OS and
// architecture, and returns the digest of the selected candidate. If a targetVariant
// is specified, it filters to only candidates matching that variant. If multiple
// candidates exist with differing variants and no targetVariant is specified,
// it returns errAmbiguousPlatformMatch.
//
// Parameters:
//   - index: The parsed image index.
//   - targetOS: Target OS for matching.
//   - targetArch: Target architecture for matching.
//   - targetVariant: Target variant for matching (empty for any variant).
//   - fields: Logging fields for context.
//
// Returns:
//   - string: The selected platform manifest digest.
//   - error: errNoPlatformMatch if no candidates, errAmbiguousPlatformMatch if ambiguous.
func selectPlatformCandidate(
	index imageIndex,
	targetOS, targetArch, targetVariant string,
	fields logrus.Fields,
) (string, error) {
	type candidate struct {
		digest  string
		variant string
	}

	// Collect all matching non-attestation platform entries.
	var candidates []candidate

	for _, entry := range index.Manifests {
		// Skip attestation manifests.
		if entry.Annotations != nil {
			if refType, ok := entry.Annotations["vnd.docker.reference.type"]; ok && refType == "attestation-manifest" {
				continue
			}
		}

		if entry.Platform.OS == targetOS && entry.Platform.Architecture == targetArch {
			candidates = append(candidates, candidate{
				digest:  entry.Digest,
				variant: entry.Platform.Variant,
			})
		}
	}

	if len(candidates) == 0 {
		logrus.WithFields(fields).
			WithField("os", targetOS).
			WithField("arch", targetArch).
			Debug("No matching platform found in image index")

		return "", errNoPlatformMatch
	}

	// If a target variant is specified, filter candidates to only those matching.
	if targetVariant != "" {
		var variantCandidates []candidate

		for _, c := range candidates {
			if c.variant == targetVariant {
				variantCandidates = append(variantCandidates, c)
			}
		}

		if len(variantCandidates) == 1 {
			selectedDigest := variantCandidates[0].digest

			logrus.WithFields(fields).
				WithField("digest", selectedDigest).
				WithField("variant", targetVariant).
				Debug("Selected platform manifest by variant from index")

			return selectedDigest, nil
		}

		if len(variantCandidates) == 0 {
			// No candidates match the requested variant; fall through to ambiguity check.
			logrus.WithFields(fields).
				WithField("target_variant", targetVariant).
				Debug("No platform entries match requested variant, checking ambiguity")
		}

		if len(variantCandidates) > 1 {
			// Multiple entries with the same variant - return the first one.
			selectedDigest := variantCandidates[0].digest

			logrus.WithFields(fields).
				WithField("digest", selectedDigest).
				WithField("variant", targetVariant).
				Debug("Selected first matching variant platform manifest from index")

			return selectedDigest, nil
		}
	}

	// If multiple candidates exist, check that all variant values are identical.
	if len(candidates) > 1 {
		firstVariant := candidates[0].variant

		for _, c := range candidates[1:] {
			if c.variant != firstVariant {
				variants := make([]string, 0, len(candidates))

				for _, c := range candidates {
					variants = append(variants, c.variant)
				}

				logrus.WithFields(fields).
					WithField("os", targetOS).
					WithField("arch", targetArch).
					WithField("variants", variants).
					Debug("Ambiguous platform match: multiple entries with differing variants")

				return "", errAmbiguousPlatformMatch
			}
		}
	}

	selectedDigest := candidates[0].digest

	logrus.WithFields(fields).
		WithField("digest", selectedDigest).
		Debug("Selected platform manifest from index")

	return selectedDigest, nil
}

// fetchPlatformManifestWithRetry fetches the platform-specific manifest by digest
// with host-based retry on 401/404 responses.
//
// It constructs the platform manifest URL from the parsed base URL and selected digest,
// then iterates through candidate hosts. On non-redirected registries, a special
// challenge-host retry is attempted when the original host fails.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - parsedURL: Parsed base manifest URL for path construction.
//   - token: Authentication token.
//   - selectedDigest: Platform manifest digest to fetch.
//   - challengeHost: Challenge host for fallback.
//   - fields: Logging fields for context.
//
// Returns:
//   - string: The config digest from the platform-specific manifest.
//   - string: The host that successfully served the manifest.
//   - error: Non-nil if all hosts fail.
func fetchPlatformManifestWithRetry(
	ctx context.Context,
	client auth.Client,
	parsedURL *url.URL,
	token, selectedDigest, challengeHost string,
	fields logrus.Fields,
) (string, string, error) {
	// Build URL path for platform-specific manifest.
	idx := strings.LastIndex(parsedURL.Path, "/manifests/")

	if idx == -1 {
		logrus.WithFields(fields).
			WithField("path", parsedURL.Path).
			Debug("Could not find /manifests/ in URL path")

		return "",
			"",
			fmt.Errorf(
				"%w: malformed manifest URL path %q",
				errFetchManifestFailed,
				parsedURL.Path,
			)
	}

	platformPathPrefix := parsedURL.Path[:idx+len("/manifests/")]

	// Build list of hosts to try: primary (current), then challenge, then original.
	// This mirrors the retry chain in digest.go's HandleManifestResponse.
	type hostCandidate struct {
		host string
		name string
	}

	originalHost := parsedURL.Host

	hosts := []hostCandidate{{host: parsedURL.Host, name: "primary"}}

	if challengeHost != "" && challengeHost != parsedURL.Host {
		hosts = append(hosts, hostCandidate{host: challengeHost, name: "challenge"})
	}

	var lastErr error

	for i, candidate := range hosts {
		// Build platform URL for this host attempt.
		attemptURL := *parsedURL
		attemptURL.Host = candidate.host
		attemptURL.Path = platformPathPrefix + selectedDigest

		if i > 0 {
			logrus.WithFields(fields).
				WithField("attempt_host", candidate.host).
				WithField("attempt_name", candidate.name).
				Debug("Retrying platform manifest fetch on alternate host")
		}

		// Fetch the platform-specific manifest.
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			attemptURL.String(),
			nil,
		)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to create platform manifest request")

			return "",
				"",
				fmt.Errorf(
					"%w: %w",
					errFetchManifestFailed,
					err,
				)
		}

		if token != "" {
			req.Header.Set("Authorization", token)
		}

		req.Header.Set(
			"Accept",
			strings.Join(
				[]string{
					"application/vnd.oci.image.manifest.v1+json",
					"application/vnd.docker.distribution.manifest.v2+json",
				},
				", "),
		)
		req.Header.Set("User-Agent", meta.UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to execute platform manifest request")

			return "",
				"",
				fmt.Errorf(
					"%w: %w",
					errFetchManifestFailed,
					err,
				)
		}

		// Handle non-success responses with retry logic.
		if resp.StatusCode != http.StatusOK {
			status := resp.StatusCode
			resp.Body.Close()

			// Retry on 401/404 if there are more hosts to try.
			if (status == http.StatusUnauthorized || status == http.StatusNotFound) &&
				i < len(hosts)-1 {
				logrus.WithFields(fields).
					WithField("status", status).
					WithField("host", candidate.host).
					Debug("Platform manifest request failed, trying next host")

				lastErr = fmt.Errorf(
					"%w: status %d on %s",
					errFetchManifestFailed,
					status,
					candidate.host,
				)

				continue
			}

			// On non-redirected registries, try challenge host for 401/404 on original.
			if (status == http.StatusUnauthorized || status == http.StatusNotFound) &&
				challengeHost != "" && candidate.host == originalHost {
				logrus.WithFields(fields).
					WithField("status", status).
					Debug("Platform manifest failed on original host, trying challenge host")

				retryURL := *parsedURL
				retryURL.Host = challengeHost
				retryURL.Path = platformPathPrefix + selectedDigest

				retryReq, retryErr := http.NewRequestWithContext(
					ctx,
					http.MethodGet,
					retryURL.String(),
					nil,
				)
				if retryErr != nil {
					return "",
						"",
						fmt.Errorf("%w: %w", errFetchManifestFailed, retryErr)
				}

				if token != "" {
					retryReq.Header.Set("Authorization", token)
				}

				retryReq.Header.Set(
					"Accept",
					strings.Join(
						[]string{
							"application/vnd.oci.image.manifest.v1+json",
							"application/vnd.docker.distribution.manifest.v2+json",
						},
						", "),
				)
				retryReq.Header.Set("User-Agent", meta.UserAgent)

				retryResp, retryErr := client.Do(retryReq)
				if retryErr != nil {
					return "",
						"",
						fmt.Errorf("%w: %w", errFetchManifestFailed, retryErr)
				}

				if retryResp.StatusCode != http.StatusOK {
					retryStatus := retryResp.StatusCode
					retryResp.Body.Close()

					logrus.WithFields(fields).
						WithField("status", retryStatus).
						Debug("Platform manifest retry also failed")

					return "",
						"",
						fmt.Errorf(
							"%w: status %d",
							errFetchManifestFailed,
							retryStatus,
						)
				}

				var retryManifest imageManifest

				err = json.NewDecoder(io.LimitReader(retryResp.Body, maxManifestSize)).Decode(&retryManifest)
				retryResp.Body.Close()

				if err != nil {
					logrus.WithError(err).
						WithFields(fields).
						Debug("Failed to parse platform manifest JSON")

					return "",
						"",
						fmt.Errorf("%w: %w", errFetchManifestFailed, err)
				}

				if retryManifest.Config.Digest == "" {
					logrus.WithFields(fields).
						Debug("Platform manifest does not contain config digest")

					return "", "", errNoConfigDigest
				}

				return retryManifest.Config.Digest, challengeHost, nil
			}

			logrus.WithFields(fields).
				WithField("status", status).
				Debug("Platform manifest request returned non-OK status")

			return "",
				"",
				fmt.Errorf(
					"%w: status %d",
					errFetchManifestFailed,
					status,
				)
		}

		var manifest imageManifest

		err = json.NewDecoder(io.LimitReader(resp.Body, maxManifestSize)).Decode(&manifest)
		resp.Body.Close()

		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to parse platform manifest JSON")

			return "",
				"",
				fmt.Errorf(
					"%w: %w",
					errFetchManifestFailed,
					err,
				)
		}

		if manifest.Config.Digest == "" {
			logrus.WithFields(fields).
				Debug("Platform manifest does not contain config digest")

			return "", "", errNoConfigDigest
		}

		return manifest.Config.Digest, candidate.host, nil
	}

	// All hosts exhausted.
	if lastErr != nil {
		return "", "", lastErr
	}

	return "",
		"",
		fmt.Errorf("%w: no hosts to try", errFetchManifestFailed)
}

// selectPlatformManifest selects the platform-specific manifest from an image index
// and fetches it to extract the config digest.
//
// If targetOS or targetArch are empty, runtime.GOOS and runtime.GOARCH are used as defaults,
// allowing cross-platform monitoring via environment variables or configuration overrides.
// If targetVariant is specified, it will be used to disambiguate when multiple entries
// match the same OS/architecture with different variants.
//
// On 401/404 responses, it retries on the challenge host mirroring the retry logic
// in digest.go's HandleManifestResponse.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - index: The parsed image index.
//   - parsedURL: Parsed URL for constructing platform manifest URLs.
//   - token: Authentication token.
//   - targetOS: Target OS for platform selection (empty for runtime.GOOS).
//   - targetArch: Target architecture for platform selection (empty for runtime.GOARCH).
//   - targetVariant: Target variant for platform selection (empty for any variant).
//   - challengeHost: Challenge host from auth (empty if not applicable).
//   - fields: Logging fields.
//
// Returns:
//   - string: The config digest from the platform-specific manifest.
//   - string: The host that successfully served the manifest.
//   - error: Non-nil if selection or fetching fails.
func selectPlatformManifest(
	ctx context.Context,
	client auth.Client,
	index imageIndex,
	parsedURL *url.URL,
	token string,
	targetOS, targetArch, targetVariant, challengeHost string,
	fields logrus.Fields,
) (string, string, error) {
	// Use runtime defaults if no override is specified.
	if targetOS == "" {
		targetOS = runtime.GOOS
	}

	if targetArch == "" {
		targetArch = runtime.GOARCH
	}

	selectedDigest, err := selectPlatformCandidate(
		index,
		targetOS,
		targetArch,
		targetVariant,
		fields,
	)
	if err != nil {
		return "", "", err
	}

	return fetchPlatformManifestWithRetry(
		ctx,
		client,
		parsedURL,
		token,
		selectedDigest,
		challengeHost,
		fields,
	)
}

// fetchConfigBlob fetches the image config blob from the registry.
// It follows redirects (307) as both GHCR and Docker Hub redirect blob GET requests.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - parsedURL: Parsed manifest URL for constructing blob URL.
//   - configDigest: Digest of the config blob.
//   - token: Authentication token.
//   - fields: Logging fields.
//
// Returns:
//   - io.ReadCloser: The config blob body (caller must close).
//   - error: Non-nil if fetching fails.
func fetchConfigBlob(
	ctx context.Context,
	client auth.Client,
	parsedURL *url.URL,
	configDigest, token string,
	fields logrus.Fields,
) (io.ReadCloser, error) {
	// Extract image path from the manifest URL.
	// Manifest URL path is: /v2/{image_path}/manifests/{tag}
	// Blob URL path should be: /v2/{image_path}/blobs/{digest}
	path := parsedURL.Path
	// Find the last /manifests/ and replace with /blobs/ to correctly handle
	// image paths that contain "/manifests/" as a component.
	idx := strings.LastIndex(path, "/manifests/")
	if idx == -1 {
		logrus.WithFields(fields).
			Debug("Could not parse image path from manifest URL")

		return nil, errFetchConfigFailed
	}

	imagePath := path[:idx]
	blobPath := imagePath + "/blobs/" + configDigest

	blobURL := *parsedURL
	blobURL.Path = blobPath

	logrus.WithFields(fields).
		WithField("blob_url", blobURL.String()).
		Debug("Fetching config blob")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		blobURL.String(),
		nil,
	)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to create config blob request")

		return nil,
			fmt.Errorf(
				"%w: %w",
				errFetchConfigFailed,
				err,
			)
	}

	if token != "" {
		req.Header.Set("Authorization", token)
	}

	req.Header.Set("User-Agent", meta.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to execute config blob request")

		return nil,
			fmt.Errorf("%w: %w", errFetchConfigFailed, err)
	}

	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound {
		// Follow redirect.
		location := resp.Header.Get("Location")
		resp.Body.Close()

		if location == "" {
			logrus.WithFields(fields).
				Debug("Redirect response missing Location header")

			return nil, errFetchConfigFailed
		}

		// Resolve relative Location against the current request URL.
		locationURL, err := url.Parse(location)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to parse redirect Location header")

			return nil, fmt.Errorf("%w: %w", errFetchConfigFailed, err)
		}

		resolvedURL := resp.Request.URL.ResolveReference(locationURL)

		redirectReq, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			resolvedURL.String(),
			nil,
		)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to create redirect request")

			return nil,
				fmt.Errorf("%w: %w", errFetchConfigFailed, err)
		}

		redirectReq.Header.Set("User-Agent", meta.UserAgent)

		// Copy Authorization header for same-origin redirects.
		if resolvedURL.Scheme == resp.Request.URL.Scheme &&
			resolvedURL.Host == resp.Request.URL.Host {
			if auth := resp.Request.Header.Get("Authorization"); auth != "" {
				redirectReq.Header.Set("Authorization", auth)
			}
		}

		resp, err = client.Do(redirectReq)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to execute redirect request")

			return nil,
				fmt.Errorf("%w: %w", errFetchConfigFailed, err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		logrus.WithFields(fields).
			WithField("status", resp.StatusCode).
			Debug("Config blob request returned non-OK status")

		return nil,
			fmt.Errorf(
				"%w: status %d",
				errFetchConfigFailed,
				resp.StatusCode,
			)
	}

	return resp.Body, nil
}

// isIndexMediaType checks if the media type indicates an image index (multi-platform).
func isIndexMediaType(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}

// buildManifestURLForContainer constructs a manifest URL for the given container's image.
//
// Parameters:
//   - container: Container whose image manifest URL to build.
//   - scheme: URL scheme (http or https).
//
// Returns:
//   - string: The manifest URL.
//   - error: Non-nil if URL construction fails.
func buildManifestURLForContainer(container types.Container, scheme string) (string, error) {
	normalizedRef, err := reference.ParseDockerRef(container.ImageName())
	if err != nil {
		return "", fmt.Errorf("failed to parse image name: %w", err)
	}

	normalizedTaggedRef, isTagged := normalizedRef.(reference.NamedTagged)
	if !isTagged {
		return "",
			fmt.Errorf(
				"%w: %s",
				errMissingTag,
				normalizedRef.String(),
			)
	}

	host := reference.Domain(normalizedTaggedRef)
	img := reference.Path(normalizedTaggedRef)
	tag := normalizedTaggedRef.Tag()

	// Map Docker Hub's default domain.
	if host == "docker.io" {
		host = "index.docker.io"
	}

	parsedManifestURL := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", img, tag),
	}

	return parsedManifestURL.String(), nil
}
