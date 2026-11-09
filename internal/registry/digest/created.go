package digest

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

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/registry/auth"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
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
)

// maxConfigSize is the maximum allowed size for a config blob response body
// (4 MiB), to prevent unbounded memory allocation.
const maxConfigSize = 1 << 22

// imageIndex represents a multi-platform image index (OCI or Docker manifest list).
type imageIndex struct {
	MediaType     string       `json:"mediaType"`
	Manifests     []indexEntry `json:"manifests"`
	SchemaVersion int          `json:"schemaVersion"`
}

// indexEntry represents a single entry in an image index.
type indexEntry struct {
	Annotations map[string]string `json:"annotations,omitempty"`
	Platform    platform          `json:"platform"`
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
}

// platform represents the platform of an image in an index.
type platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// imageManifest represents a single-platform image manifest (OCI or Docker v2).
type imageManifest struct {
	MediaType string `json:"mediaType"`
	Config    struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	} `json:"config"`
	SchemaVersion int `json:"schemaVersion"`
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
// The cooldown platform in settings selects the platform manifest. Its OS and
// architecture default to the runtime platform, which allows cross-platform
// monitoring, and its variant (e.g., "v7", "v8") disambiguates ARM images with
// multiple variants.
//
// This function does NOT download the image layers — only the manifest and config
// blob (typically <15KB total) are fetched.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - c: Container whose image creation time to fetch.
//   - registryAuth: Base64-encoded registry credentials.
//   - settings: Registry request settings.
//   - limiter: Rate limiter shared by registry requests. Must not be nil.
//
// Returns:
//   - time.Time: The image creation timestamp from the registry config.
//   - error: Non-nil if any step fails.
func FetchImageCreationTime(log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	registryAuth string,
	settings registryConfig.Registry,
	limiter *ratelimit.Limiter,
) (time.Time, error) {
	fields := map[string]any{
		"container": c.Name(),
		"image":     c.ImageName(),
	}

	// Determine target platform (supports cross-platform monitoring overrides).
	targetOS := settings.CooldownPlatform.OS
	targetArch := settings.CooldownPlatform.Architecture
	targetVariant := settings.CooldownPlatform.Variant

	// Transform auth credentials into usable format.
	registryAuth = auth.TransformAuth(log, registryAuth)

	// Use the cached HTTP client for registry requests.
	client := auth.NewAuthClient(log, settings)

	_, originalHost, parsedURL, err := buildManifestURL(log, c, settings, "")
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	limitKey, release, holdErr := limiter.HoldAnonymous(ctx, originalHost, registryAuth != "")
	if holdErr != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errFetchManifestFailed, holdErr)
	}
	defer release()

	// Obtain an authentication token for the registry.
	result, err := limiter.DoValue(ctx, log, limitKey, func() (auth.TokenResult, error) {
		return auth.GetToken(log,
			ctx,
			c,
			registryAuth,
			client,
			settings,
			"",
		)
	})
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to get auth token for image age check")

		return time.Time{},
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	token := result.Token

	// A challenge redirected to another host sends the manifest request there
	// first, with the original host as the fallback.
	if redirectHost := challengeRedirectHost(result, originalHost); redirectHost != "" {
		parsedURL.Host = redirectHost
	}

	configDigest, manifestURL, err := fetchManifestForAge(log,
		ctx,
		client,
		limiter,
		token,
		parsedURL,
		targetOS,
		targetArch,
		targetVariant,
		originalHost,
		limitKey,
		fields,
	)
	if err != nil {
		return time.Time{}, err
	}

	log.Debug().
		Fields(fields).
		Str("config_digest", configDigest).
		Msg("Extracted config digest from manifest")

	// The config blob comes from the host that served the manifest.
	configBody, err := fetchConfigBlob(log,
		ctx,
		client,
		limiter,
		manifestURL,
		configDigest,
		token,
		limitKey,
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
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to parse image config JSON")

		return time.Time{},
			fmt.Errorf("%w: %w", errParseConfigFailed, err)
	}

	// Check if the created field is present.
	if config.Created == nil {
		log.Debug().
			Fields(fields).
			Msg("Image config does not contain creation timestamp")

		return time.Time{}, errImageCreationTimeMissing
	}

	log.Debug().
		Fields(fields).
		Time("created", *config.Created).
		Msg("Fetched image creation time from registry")

	return *config.Created, nil
}

// fetchManifestAcrossHosts fetches manifest bytes from the registry with host-based retry.
//
// It requests the manifest from the host in parsedURL. When that host is the
// one the registry's challenge redirected to and it answers 401 or 404, it
// retries once on originalHost, as the digest check does.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - limiter: Rate limiter shared by registry requests.
//   - parsedURL: Parsed manifest URL, on the redirect host when there is one.
//   - token: Authentication token for the Authorization header.
//   - originalHost: The image registry's host.
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//   - fields: Logging fields for context.
//
// Returns:
//   - []byte: Raw manifest response body bytes.
//   - *url.URL: The manifest URL that served the manifest.
//   - string: The HTTP Content-Type header value from the response.
//   - error: Non-nil if the request fails on every host tried.
func fetchManifestAcrossHosts(log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	parsedURL *url.URL,
	token, originalHost, limitKey string,
	fields map[string]any,
) ([]byte, *url.URL, string, error) {
	body, contentType, status, err := fetchManifest(log, ctx, client, limiter, parsedURL, token, limitKey, fields)
	if err == nil {
		return body, parsedURL, contentType, nil
	}

	if !retriesOnOriginalHost(status, parsedURL.Host, originalHost) {
		return nil, nil, "", err
	}

	log.Debug().
		Fields(fields).
		Str("original_host", originalHost).
		Msg("Manifest request failed on the redirect host, retrying on the original host")

	originalURL := *parsedURL
	originalURL.Host = originalHost

	body, contentType, _, err = fetchManifest(log, ctx, client, limiter, &originalURL, token, limitKey, fields)
	if err != nil {
		return nil, nil, "", err
	}

	return body, &originalURL, contentType, nil
}

// fetchManifest fetches manifest bytes from the host in manifestURL.
//
// It returns the raw body of an HTTP 200. Any other status fails. A body over
// 1 MiB fails too.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - limiter: Rate limiter shared by registry requests.
//   - manifestURL: URL of the manifest to fetch.
//   - token: Authentication token for the Authorization header.
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//   - fields: Logging fields for context.
//
// Returns:
//   - []byte: Raw manifest response body bytes.
//   - string: The HTTP Content-Type header value from the response.
//   - int: The response status. Zero when no response arrived.
//   - error: Non-nil unless a 200 response with a body of at most 1 MiB arrives.
func fetchManifest(log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	manifestURL *url.URL,
	token, limitKey string,
	fields map[string]any,
) ([]byte, string, int, error) {
	req, err := makeManifestRequest(ctx, http.MethodGet, manifestURL.String(), token)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to create manifest request")

		return nil, "", 0, fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	resp, err := doRegistryRequest(log, ctx, client, limiter, req, limitKey)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to execute manifest request")

		if ratelimit.Is(err) {
			return nil, "", 0, err
		}

		return nil, "", 0, fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debug().
			Fields(fields).
			Int("status", resp.StatusCode).
			Str("host", manifestURL.Host).
			Msg("Manifest request returned non-OK status")

		return nil, "", resp.StatusCode, fmt.Errorf(
			"%w: status %d on %s",
			errFetchManifestFailed,
			resp.StatusCode,
			manifestURL.Host,
		)
	}

	body, err := readManifestBody(resp.Body)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to read manifest response")

		return nil, "", resp.StatusCode, fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	return body, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

// fetchManifestForAge fetches the image manifest and extracts the config digest.
// It handles multi-platform indexes by selecting the platform-specific manifest.
//
// On 401/404 responses from the host the registry's challenge redirected to,
// it retries on the original host.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - limiter: Rate limiter shared by registry requests.
//   - token: Authentication token.
//   - parsedURL: Parsed manifest URL.
//   - targetOS: Target OS for platform selection (empty for runtime.GOOS).
//   - targetArch: Target architecture for platform selection (empty for runtime.GOARCH).
//   - targetVariant: Target variant for platform selection (empty for any variant).
//   - originalHost: The image registry's host.
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//   - fields: Logging fields for context.
//
// Returns:
//   - string: Config digest (e.g., "sha256:abc...").
//   - *url.URL: The manifest URL that served the manifest.
//   - error: Non-nil if fetch, parse, or platform selection fails.
func fetchManifestForAge(log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	token string,
	parsedURL *url.URL,
	targetOS, targetArch, targetVariant, originalHost, limitKey string,
	fields map[string]any,
) (string, *url.URL, error) {
	body, servedURL, contentType, err := fetchManifestAcrossHosts(log,
		ctx,
		client,
		limiter,
		parsedURL,
		token,
		originalHost,
		limitKey,
		fields,
	)
	if err != nil {
		return "", nil, err
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
		idx := strings.Index(contentType, ";")
		if idx != -1 {
			contentType = strings.TrimSpace(contentType[:idx])
		}

		effectiveMediaType = contentType
	}

	if idxErr == nil && isIndexMediaType(effectiveMediaType) {
		// The platform manifest is fetched from the host that served the index.
		configDigest, err := selectPlatformManifest(log,
			ctx,
			client,
			limiter,
			index,
			servedURL,
			token,
			targetOS,
			targetArch,
			targetVariant,
			limitKey,
			fields,
		)
		if err != nil {
			return "", nil, err
		}

		return configDigest, servedURL, nil
	}

	// Parse as single-platform manifest.
	var manifest imageManifest

	err = json.Unmarshal(body, &manifest)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to parse manifest JSON")

		return "", nil, fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	if manifest.Config.Digest == "" {
		log.Debug().
			Fields(fields).
			Msg("Manifest does not contain config digest")

		return "", nil, errNoConfigDigest
	}

	return manifest.Config.Digest, servedURL, nil
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
func selectPlatformCandidate(log *zerolog.Logger,
	index imageIndex,
	targetOS, targetArch, targetVariant string,
	fields map[string]any,
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
			refType, ok := entry.Annotations["vnd.docker.reference.type"]
			if ok && refType == "attestation-manifest" {
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
		log.Debug().
			Fields(fields).
			Str("os", targetOS).
			Str("arch", targetArch).
			Msg("No matching platform found in image index")

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

			log.Debug().
				Fields(fields).
				Str("digest", selectedDigest).
				Str("variant", targetVariant).
				Msg("Selected platform manifest by variant from index")

			return selectedDigest, nil
		}

		if len(variantCandidates) == 0 {
			log.Debug().
				Fields(fields).
				Str("target_variant", targetVariant).
				Msg("No platform entries match requested variant")

			return "", errNoPlatformMatch
		}

		if len(variantCandidates) > 1 {
			// Multiple entries with the same variant. Return the first one.
			selectedDigest := variantCandidates[0].digest

			log.Debug().
				Fields(fields).
				Str("digest", selectedDigest).
				Str("variant", targetVariant).
				Msg("Selected first matching variant platform manifest from index")

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

				log.Debug().
					Fields(fields).
					Str("os", targetOS).
					Str("arch", targetArch).
					Strs("variants", variants).
					Msg("Ambiguous platform match: multiple entries with differing variants")

				return "", errAmbiguousPlatformMatch
			}
		}
	}

	selectedDigest := candidates[0].digest

	log.Debug().
		Fields(fields).
		Str("digest", selectedDigest).
		Msg("Selected platform manifest from index")

	return selectedDigest, nil
}

// fetchPlatformManifest fetches the platform-specific manifest by digest from
// the host in parsedURL and returns its config digest.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - limiter: Rate limiter shared by registry requests.
//   - parsedURL: Parsed base manifest URL for the host and path.
//   - token: Authentication token.
//   - selectedDigest: Platform manifest digest to fetch.
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//   - fields: Logging fields for context.
//
// Returns:
//   - string: The config digest from the platform-specific manifest.
//   - error: Non-nil if the fetch fails or the manifest has no config digest.
func fetchPlatformManifest(log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	parsedURL *url.URL,
	token, selectedDigest, limitKey string,
	fields map[string]any,
) (string, error) {
	// Build URL path for platform-specific manifest.
	idx := strings.LastIndex(parsedURL.Path, "/manifests/")

	if idx == -1 {
		log.Debug().
			Fields(fields).
			Str("path", parsedURL.Path).
			Msg("Could not find /manifests/ in URL path")

		return "", fmt.Errorf(
			"%w: malformed manifest URL path %q",
			errFetchManifestFailed,
			parsedURL.Path,
		)
	}

	platformURL := *parsedURL
	platformURL.Path = parsedURL.Path[:idx+len("/manifests/")] + selectedDigest

	body, _, _, err := fetchManifest(log, ctx, client, limiter, &platformURL, token, limitKey, fields)
	if err != nil {
		return "", err
	}

	var manifest imageManifest

	err = json.Unmarshal(body, &manifest)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to parse platform manifest JSON")

		return "", fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	if manifest.Config.Digest == "" {
		log.Debug().
			Fields(fields).
			Msg("Platform manifest does not contain config digest")

		return "", errNoConfigDigest
	}

	return manifest.Config.Digest, nil
}

// selectPlatformManifest selects the platform-specific manifest from an image index
// and fetches it to extract the config digest.
//
// If targetOS or targetArch are empty, runtime.GOOS and runtime.GOARCH are used as defaults,
// allowing cross-platform monitoring via environment variables or configuration overrides.
// If targetVariant is specified, it will be used to disambiguate when multiple entries
// match the same OS/architecture with different variants.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - limiter: Rate limiter shared by registry requests.
//   - index: The parsed image index.
//   - parsedURL: Parsed URL for constructing platform manifest URLs.
//   - token: Authentication token.
//   - targetOS: Target OS for platform selection (empty for runtime.GOOS).
//   - targetArch: Target architecture for platform selection (empty for runtime.GOARCH).
//   - targetVariant: Target variant for platform selection (empty for any variant).
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//   - fields: Logging fields.
//
// Returns:
//   - string: The config digest from the platform-specific manifest.
//   - error: Non-nil if selection or fetching fails.
func selectPlatformManifest(
	log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	index imageIndex,
	parsedURL *url.URL,
	token string,
	targetOS, targetArch, targetVariant, limitKey string,
	fields map[string]any,
) (string, error) {
	// Use runtime defaults if no override is specified.
	if targetOS == "" {
		targetOS = runtime.GOOS
	}

	if targetArch == "" {
		targetArch = runtime.GOARCH
	}

	selectedDigest, err := selectPlatformCandidate(log,
		index,
		targetOS,
		targetArch,
		targetVariant,
		fields,
	)
	if err != nil {
		return "", err
	}

	return fetchPlatformManifest(log,
		ctx,
		client,
		limiter,
		parsedURL,
		token,
		selectedDigest,
		limitKey,
		fields,
	)
}

// fetchConfigBlob fetches the image config blob from the registry.
// It follows redirects (307) as both GHCR and Docker Hub redirect blob GET requests.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - limiter: Rate limiter shared by registry requests.
//   - parsedURL: Parsed manifest URL for constructing blob URL.
//   - configDigest: Digest of the config blob.
//   - token: Authentication token.
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//   - fields: Logging fields.
//
// Returns:
//   - io.ReadCloser: The config blob body (caller must close).
//   - error: Non-nil if fetching fails.
func fetchConfigBlob(log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	parsedURL *url.URL,
	configDigest, token, limitKey string,
	fields map[string]any,
) (io.ReadCloser, error) {
	// Extract image path from the manifest URL.
	// Manifest URL path is: /v2/{image_path}/manifests/{tag}
	// Blob URL path should be: /v2/{image_path}/blobs/{digest}
	path := parsedURL.Path
	// Find the last /manifests/ and replace with /blobs/ to correctly handle
	// image paths that contain "/manifests/" as a component.
	before, _, ok := strings.CutLast(path, "/manifests/")
	if !ok {
		log.Debug().
			Fields(fields).
			Msg("Could not parse image path from manifest URL")

		return nil, errFetchConfigFailed
	}

	imagePath := before
	blobPath := imagePath + "/blobs/" + configDigest

	blobURL := *parsedURL
	blobURL.Path = blobPath

	log.Debug().
		Fields(fields).
		Str("blob_url", blobURL.String()).
		Msg("Fetching config blob")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		blobURL.String(),
		nil,
	)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to create config blob request")

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

	resp, err := doRegistryRequest(log, ctx, client, limiter, req, limitKey)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to execute config blob request")

		if ratelimit.Is(err) {
			return nil, err
		}

		return nil,
			fmt.Errorf("%w: %w", errFetchConfigFailed, err)
	}

	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound {
		// Follow redirect.
		location := resp.Header.Get("Location")
		resp.Body.Close()

		if location == "" {
			log.Debug().
				Fields(fields).
				Msg("Redirect response missing Location header")

			return nil, errFetchConfigFailed
		}

		// Resolve relative Location against the current request URL.
		locationURL, err := url.Parse(location)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to parse redirect Location header")

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
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to create redirect request")

			return nil,
				fmt.Errorf("%w: %w", errFetchConfigFailed, err)
		}

		redirectReq.Header.Set("User-Agent", meta.UserAgent)

		// Copy Authorization header for same-origin redirects.
		if resolvedURL.Scheme == resp.Request.URL.Scheme &&
			resolvedURL.Host == resp.Request.URL.Host {
			auth := resp.Request.Header.Get("Authorization")
			if auth != "" {
				redirectReq.Header.Set("Authorization", auth)
			}
		}

		resp, err = doRegistryRequest(log, ctx, client, limiter, redirectReq, limitKey)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to execute redirect request")

			if ratelimit.Is(err) {
				return nil, err
			}

			return nil,
				fmt.Errorf("%w: %w", errFetchConfigFailed, err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		log.Debug().
			Fields(fields).
			Int("status", resp.StatusCode).
			Msg("Config blob request returned non-OK status")

		return nil,
			fmt.Errorf(
				"%w: status %d",
				errFetchConfigFailed,
				resp.StatusCode,
			)
	}

	return resp.Body, nil
}

// doRegistryRequest executes req through [ratelimit.Limiter.Do] so 429s honor the retry window.
//
// Observation is left to [ratelimit.Limiter.Do]. This helper only returns a parsed
// [ratelimit.Error] so nested callers do not Observe twice.
//
// Parameters:
//   - log: Logger for retry notices. May be nil.
//   - ctx: Context that bounds the retry loop.
//   - client: HTTP client for the registry request.
//   - limiter: Rate limiter shared by registry requests.
//   - req: Request to execute. GET requests may be retried.
//   - limitKey: Rate-limit host key from [ratelimit.Scope].
//
// Returns:
//   - *http.Response: Successful response. The caller must close the body.
//   - error: [ratelimit.Error] when retries are exhausted, or the transport error.
func doRegistryRequest(
	log *zerolog.Logger,
	ctx context.Context,
	client auth.Client,
	limiter *ratelimit.Limiter,
	req *http.Request,
	limitKey string,
) (*http.Response, error) {
	var resp *http.Response

	err := limiter.Do(ctx, log, limitKey, func() error {
		got, doErr := client.Do(req)
		if doErr != nil {
			return fmt.Errorf("registry request: %w", doErr)
		}

		rateErr := registryRateLimitError(got)
		if rateErr != nil {
			_ = got.Body.Close()

			return rateErr
		}

		resp = got

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("registry request: %w", err)
	}

	return resp, nil
}

// registryRateLimitError returns a rate-limit error when the registry responded 429.
//
// It parses the response and does not call [ratelimit.Limiter.Observe]. Callers that
// retry through [ratelimit.Limiter.Do] rely on that loop to record the 429 once.
//
// Parameters:
//   - resp: HTTP response from a registry request.
//
// Returns:
//   - error: Parsed rate-limit error, or nil when the status is not 429.
func registryRateLimitError(resp *http.Response) error {
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}

	return ratelimit.FromResponse(resp, ratelimit.ReadBody(resp, ratelimit.DefaultBodyLimit))
}

// isIndexMediaType checks if the media type indicates an image index (multi-platform).
func isIndexMediaType(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}
