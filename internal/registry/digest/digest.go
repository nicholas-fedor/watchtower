package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/registry/auth"
	"github.com/nicholas-fedor/watchtower/internal/registry/manifest"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
)

// ContentDigestHeader is the HTTP header key used to retrieve the digest from a registry's response.
// This header, typically "Docker-Content-Digest", contains the digest value (e.g., "sha256:abc...") for an image manifest,
// allowing Watchtower to compare or fetch it without downloading the full manifest body.
const ContentDigestHeader = "Docker-Content-Digest"

// maxManifestSize is the maximum allowed size for a manifest response body (1 MiB).
const maxManifestSize = 1 << 20

// schema2Version is the schemaVersion of OCI and Docker schema 2 manifests and
// indexes, whose digest is the sha256 of their bytes.
const schema2Version = 2

// manifestBody holds the fields of a manifest response body that tell whether
// it carries a digest or is a schema 2 manifest or index.
type manifestBody struct {
	// Manifests lists the platform manifests of an index. Nil when absent.
	Manifests *[]json.RawMessage `json:"manifests"`
	// Config is the config descriptor of an image manifest. Nil when absent.
	Config *struct {
		// Digest is the config blob's digest.
		Digest string `json:"digest"`
	} `json:"config"`
	// Layers lists the layer descriptors of an image manifest. Nil when absent.
	Layers *[]json.RawMessage `json:"layers"`
	// Digest is a digest some registries add to the body.
	Digest string `json:"digest"`
	// SchemaVersion is 2 for OCI and Docker schema 2 manifests and indexes.
	SchemaVersion int `json:"schemaVersion"`
}

// digestLookup holds what one digest lookup shares across the registry
// endpoints it tries.
type digestLookup struct {
	// log is the process logger.
	log *zerolog.Logger
	// c is the container whose image digest is looked up.
	c *container.Container
	// client sends the token and manifest requests.
	client auth.Client
	// limiter paces registry requests.
	limiter *ratelimit.Limiter
	// fields name the container and its image in debug logs.
	fields map[string]any
	// registryAuth is the transformed registry credential, or empty.
	registryAuth string
	// endpoints are the registry endpoints in the order requests are sent to
	// them, with the outcome of the requests sent to each.
	endpoints []endpointState
	// tag replaces the image's own tag in the manifest request when set.
	tag string
	// originalHost is the image registry's host.
	originalHost string
	// limitKey is the rate limiter key the lookup holds.
	limitKey string
	// settings are the registry request settings.
	settings registryConfig.Registry
}

// endpointState records the outcome of a digest lookup's requests to one
// registry endpoint.
type endpointState struct {
	// fields name the endpoint in debug logs. They are nil for the canonical
	// host.
	fields map[string]any
	// err is the endpoint's latest failure, or nil.
	err error
	// endpoint is the registry mirror host, or empty for the canonical host.
	endpoint string
}

// manifestTarget is where and how one manifest request goes.
type manifestTarget struct {
	// parsedURL is the parsed manifest URL of the endpoint's first request,
	// whose host is the host that request goes to.
	parsedURL *url.URL
	// url is the manifest URL.
	url string
	// token is the Authorization header value, or empty when none is sent.
	token string
	// redirected reports that the canonical registry's challenge redirected the
	// request to another host.
	redirected bool
}

// manifestReply is the result of one manifest request's response.
type manifestReply struct {
	// digest is the normalized digest. It is empty when the response requires
	// a retry.
	digest string
	// retryURL is the URL at which the response requires the request to be
	// retried, or empty.
	retryURL string
	// redirect reports that the retry URL came from the registry's redirect
	// rather than a host Watchtower chose.
	redirect bool
}

// Errors for digest retrieval operations.
var (
	// errMissingImageInfo indicates the container lacks image metadata, preventing digest operations.
	errMissingImageInfo = errors.New("container image info missing")
	// errInvalidRegistryResponse indicates the registry's HEAD response lacks a digest or is malformed.
	errInvalidRegistryResponse = errors.New("registry responded with invalid HEAD request")
	// errManifestNotFound indicates the registry returned 404 for a HEAD manifest request.
	errManifestNotFound = errors.New("manifest not found")
	// errHeadFallback indicates a HEAD manifest response contained no digest, so
	// the manifest should be requested with GET.
	//
	// A failed HEAD request other than a 404 or a refused repository returns it.
	// So does a successful HEAD response whose digest header is missing or has no hash.
	errHeadFallback = errors.New("HEAD request gave no digest")
	// errRepositoryDenied indicates the registry refused access to the
	// repository. It responded to the manifest request with a 401 whose
	// challenge reports insufficient_scope.
	errRepositoryDenied = errors.New("registry denied access to the repository")
	// errFailedGetToken indicates a failure to obtain an authentication token from the registry.
	errFailedGetToken = errors.New("failed to get token")
	// errFailedBuildManifestURL indicates a failure to construct the manifest URL for the registry.
	errFailedBuildManifestURL = errors.New("failed to build manifest URL")
	// errFailedParseImageName indicates an image reference could not be parsed or retagged.
	errFailedParseImageName = errors.New("failed to parse image name")
	// errFailedCreateRequest indicates a failure to construct an HTTP request for digest retrieval.
	errFailedCreateRequest = errors.New("failed to create request")
	// errManifestTooLarge indicates a manifest response body exceeded the size limit.
	errManifestTooLarge = errors.New("manifest response exceeds size limit")
	// errUnresolvedManifestRetry indicates the retried manifest request was sent to yet another host.
	errUnresolvedManifestRetry = errors.New("manifest request still needs another host after retrying")
)

// localOnlyImageCache records image name+ID pairs confirmed as local-only after
// the registry refused the repository. Subsequent checks skip remote probes,
// avoiding Docker Hub rate-limit pressure that can starve concurrent staleness
// checks for other containers in the same update cycle.
//
// Keyed by "imageName|imageID".
// Entries live for process lifetime and a rebuild (new image ID) forces a fresh probe.
var localOnlyImageCache sync.Map

// localOnlyCacheKey builds a cache key for a container's image identity.
func localOnlyCacheKey(c *container.Container) string {
	if !c.HasImageInfo() {
		return c.ImageName() + "|"
	}

	return c.ImageName() + "|" + c.ImageInfo().ID
}

// rememberLocalOnlyImage records that the container's image was confirmed local-only.
func rememberLocalOnlyImage(c *container.Container) {
	localOnlyImageCache.Store(localOnlyCacheKey(c), true)
}

// isCachedLocalOnlyImage reports whether the container's image was previously
// confirmed local-only for this process.
func isCachedLocalOnlyImage(c *container.Container) bool {
	_, ok := localOnlyImageCache.Load(localOnlyCacheKey(c))

	return ok
}

// readManifestBody reads a manifest response body of at most 1 MiB.
//
// Parameters:
//   - body: The response body.
//
// Returns:
//   - []byte: The body bytes.
//   - error: Non-nil if the read fails or the body is larger than 1 MiB.
func readManifestBody(body io.Reader) ([]byte, error) {
	// The extra byte detects a body over the limit.
	data, err := io.ReadAll(io.LimitReader(body, maxManifestSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(data) > maxManifestSize {
		return nil, fmt.Errorf(
			"%w: %d bytes exceeds limit of %d bytes",
			errManifestTooLarge,
			len(data),
			maxManifestSize,
		)
	}

	return data, nil
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
func NormalizeDigest(log *zerolog.Logger, digest string) string {
	// List of prefixes to strip from the digest.
	prefixes := []string{"sha256:"}
	for _, prefix := range prefixes {
		after, ok := strings.CutPrefix(digest, prefix)
		if ok {
			// Trim the prefix to get the raw digest value.
			normalized := after
			log.Debug().
				Str("original", digest).
				Str("normalized", normalized).
				Msg("Normalized digest by trimming prefix")

			return normalized
		}
	}

	// Return unchanged if no prefix matches.
	return digest
}

// CompareDigest checks whether a container's current image digest matches the
// latest from its registry.
//
// It is a convenience wrapper around CompareDigestWithRemote that discards the
// remote digest.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - c: Container whose digest is being compared.
//   - registryAuth: Base64-encoded auth string.
//   - settings: Registry request settings.
//   - limiter: Rate limiter shared by registry requests. Must not be nil.
//   - endpoints: Optional list of registry mirror host overrides to try before the canonical host.
//
// Returns:
//   - bool: True if digests match (image is up-to-date), false otherwise.
//   - error: Non-nil if operation fails, nil on success.
func CompareDigest(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	registryAuth string,
	settings registryConfig.Registry,
	limiter *ratelimit.Limiter,
	endpoints ...string,
) (bool, error) {
	match, _, err := CompareDigestWithRemote(log,
		ctx,
		c,
		registryAuth,
		settings,
		limiter,
		endpoints...,
	)

	return match, err
}

// isRefusedLocalImage reports whether the digest fetch failed because the
// registry refused the repository and the container's image looks local-only.
//
// An image looks local-only when its repository name is a single component
// with no registry or namespace, such as atlas-badgerdb. None of its
// RepoDigests may name a registry host such as ghcr.io. It judges the image
// name the probe requested, which a Zodiac label sets when the container runs
// from an image ID.
//
// A manifest 404 never makes an image local-only.
//
// Parameters:
//   - c: Container whose image name is inspected.
//   - err: Error returned from digest fetch.
//
// Returns:
//   - bool: True if the image should be treated as local-only, false otherwise.
func isRefusedLocalImage(c *container.Container, err error) bool {
	// Docker Hub refuses a single-component name it does not have with a 401
	// whose challenge reports insufficient_scope, with or without credentials.
	// Its official namespace has no private repositories. The refusal therefore
	// means the repository does not exist. A 404 means the repository exists
	// without the tag.
	if !errors.Is(err, errRepositoryDenied) {
		return false
	}

	if !c.HasImageInfo() {
		return false
	}

	// A registry or namespace is always followed by a slash. Neither a tag nor
	// a digest contains one.
	if strings.Contains(c.ImageName(), "/") {
		return false
	}

	// Registry-qualified digests mean the image was pulled from a remote source,
	// so a refusal is a failed probe rather than proof the image is local-only.
	for _, digestRef := range c.ImageInfo().RepoDigests {
		repoPart, _, _ := strings.Cut(digestRef, "@")
		if repoPartHasRegistryHost(repoPart) {
			return false
		}
	}

	return true
}

// repoPartHasRegistryHost reports whether a RepoDigest repository path starts
// with a registry host.
//
// Docker counts a first component as a registry host when it is localhost or
// contains a dot, a colon or an uppercase letter.
func repoPartHasRegistryHost(repoPart string) bool {
	first, _, hasSlash := strings.Cut(repoPart, "/")
	if !hasSlash {
		// A bare name such as nginx has no host component.
		return false
	}

	return strings.ContainsAny(first, ".:") || first == "localhost" || strings.ToLower(first) != first
}

// CompareDigestWithRemote checks whether a container's current image digest matches
// the latest from its registry and returns the remote digest used for comparison.
//
// It first inspects the image to check if it's locally built (empty RepoDigests).
// For local images, digest comparison against a remote registry is not possible,
// so it returns true to indicate the image should not be updated. This avoids
// unnecessary HTTP requests and confusing error messages for locally built images.
//
// When endpoints are provided (registry mirror hosts), each is tried in order.
// An empty string in the endpoints list means use the canonical registry host.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - c: Container whose digest is being compared.
//   - registryAuth: Base64-encoded auth string.
//   - settings: Registry request settings.
//   - limiter: Rate limiter shared by registry requests. Must not be nil.
//   - endpoints: Optional list of registry mirror host overrides to try before the canonical host.
//
// Returns:
//   - bool: True if digests match (image is up-to-date), false otherwise.
//   - string: Remote registry digest in "sha256:..." form (empty when unavailable).
//   - error: Non-nil if operation fails, nil on success.
func CompareDigestWithRemote(log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	registryAuth string,
	settings registryConfig.Registry,
	limiter *ratelimit.Limiter,
	endpoints ...string,
) (bool, string, error) {
	fields := map[string]any{
		"container": c.Name(),
		"image":     c.ImageName(),
	}

	// Ensure the container has image metadata to proceed with digest comparison.
	if !c.HasImageInfo() {
		log.Debug().
			Fields(fields).
			Msg("Container image info missing")

		return false, "", errMissingImageInfo
	}

	// Check if the container's image has no RepoDigests, which indicates a locally
	// built image that has never been pushed or pulled. For such images, there is
	// no remote digest to compare against, so we treat them as up-to-date to avoid
	// unnecessary registry requests and confusing error messages.
	//
	// We check container.ImageInfo().RepoDigests rather than inspecting via the
	// Docker daemon because:
	// 1. The container was already populated with image info during initialization
	// 2. For locally built images, RepoDigests are either empty or registry-less
	// 3. This avoids an extra Docker daemon call
	if len(c.ImageInfo().RepoDigests) == 0 {
		log.Debug().
			Fields(fields).
			Msg("Image with no registry reference detected (empty RepoDigests) - skipping digest comparison")

		return true, "", nil
	}

	// Skip registry probes for images previously confirmed local-only (same name+ID).
	// This prevents repeated Docker Hub refusals that can rate-limit concurrent
	// checks for other containers in the same update session.
	if isCachedLocalOnlyImage(c) {
		log.Debug().
			Fields(fields).
			Msg("Cached local-only image - skipping digest comparison")

		return true, "", nil
	}

	lookup, release, err := newDigestLookup(log, ctx, c, registryAuth, settings, limiter, "", endpoints)
	if err != nil {
		return false, "", err
	}
	defer release()

	// HEAD requests come first, since Docker Hub does not count them toward its
	// pull limit.
	remoteDigest, err := lookup.fetch(ctx, http.MethodHead)

	// GET request fallback.
	if errors.Is(err, errHeadFallback) {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("No HEAD response gave a digest - falling back to GET")

		remoteDigest, err = lookup.fetch(ctx, http.MethodGet)
	}

	if err != nil {
		// A refused local-only image is up to date. Return no error so callers
		// skip the pull without logging a warning.
		if isRefusedLocalImage(c, err) {
			rememberLocalOnlyImage(c)
			log.Debug().
				Fields(fields).
				Msg("Registry refused the repository - treating as local image")

			return true, "", nil
		}

		return false, "", err
	}

	// Compare the fetched remote digest with the container's local digests.
	matches := digestsMatch(log,
		c.ImageInfo().RepoDigests,
		remoteDigest,
	)
	log.Debug().
		Fields(fields).
		Bool("matches", matches).
		Msg("Completed digest comparison")

	return matches, formatDigest(remoteDigest), nil
}

// formatDigest ensures a digest string uses the "sha256:..." form.
//
// Empty input is returned unchanged. Digests that already include a known
// algorithm prefix are returned as-is, otherwise "sha256:" is prepended.
//
// Parameters:
//   - digest: Digest string (raw hash or "sha256:...").
//
// Returns:
//   - string: Digest with algorithm prefix when non-empty.
func formatDigest(digest string) string {
	if digest == "" {
		return ""
	}

	if strings.Contains(digest, ":") {
		return digest
	}

	return "sha256:" + digest
}

// FetchDigestForTag retrieves the manifest digest of a specific tag.
//
// It is used to confirm which release tag a maintainer actually published, so a
// changelog link can name the exact release rather than the release index. The
// request is a HEAD: a missing digest header is not retried with a GET, because
// the probe is optional and must stay cheap.
//
// Authentication is scoped to the container's repository, which is the same for
// every tag, so only the manifest specifier changes between candidates.
//
// Parameters:
//   - log: Logger.
//   - ctx: The context controlling the request's lifecycle.
//   - c: Container whose image supplies the repository and registry host.
//   - authToken: A base64-encoded authentication string for registry access.
//   - tag: Exact tag to fetch the digest for.
//   - settings: Registry request settings.
//   - limiter: Rate limiter shared by registry requests. Must not be nil.
//   - endpoints: Optional list of registry mirror host overrides to try before the canonical host.
//
// Returns:
//   - string: The normalized digest, or empty when the registry responds with
//     404 for the tag.
//   - error: Non-nil if the request fails or no endpoint's HEAD response
//     contains a digest.
func FetchDigestForTag(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	authToken string,
	tag string,
	settings registryConfig.Registry,
	limiter *ratelimit.Limiter,
	endpoints ...string,
) (string, error) {
	// An image without repo digests was built locally, so the registry has no
	// tags for it.
	if c.HasImageInfo() && len(c.ImageInfo().RepoDigests) == 0 {
		return "", fmt.Errorf("%w: no digest for tag %s", errInvalidRegistryResponse, tag)
	}

	lookup, release, err := newDigestLookup(log, ctx, c, authToken, settings, limiter, tag, endpoints)
	if err != nil {
		return "", err
	}
	defer release()

	remote, err := lookup.fetch(ctx, http.MethodHead)

	switch {
	case errors.Is(err, errManifestNotFound):
		// The registry has the repository but not this tag.
		return "", nil
	case errors.Is(err, errHeadFallback):
		// A response without a digest does not show whether the tag exists, so
		// the caller must not read it as missing.
		return "", fmt.Errorf("%w: no digest for tag %s: %w", errInvalidRegistryResponse, tag, err)
	case err != nil:
		return "", err
	default:
		return remote, nil
	}
}

// buildManifestURL constructs and validates a manifest URL for a container.
//
// The URL names the image's tag. A digest-only reference names its digest
// instead. Its host is the image registry's host unless hostOverride is a bare
// host or an endpoint URL such as "https://mirror.example.com". An endpoint
// URL's scheme takes precedence over the registry settings.
//
// Parameters:
//   - c: Container whose manifest URL is being built.
//   - settings: Registry request settings.
//   - hostOverride: Optional host or endpoint URL to use instead of the canonical host.
//     An empty string uses the canonical host.
//
// Returns:
//   - string: The final manifest URL.
//   - string: The image registry's host, before hostOverride applies.
//   - *url.URL: The parsed URL object.
//   - error: Non-nil if construction or validation fails.
func buildManifestURL(log *zerolog.Logger,
	c *container.Container,
	settings registryConfig.Registry,
	hostOverride string,
) (string, string, *url.URL, error) {
	return buildManifestURLForImage(log, c.ImageName(), c.Name(), settings, hostOverride)
}

// buildManifestURLForTag constructs a manifest URL for a specific tag of an
// image, leaving the repository and registry host unchanged.
//
// This is how a release tag is confirmed without pulling it: the manifest for
// the candidate tag is requested and its digest compared with the digest of the
// image that was actually pulled. Authentication scope is the repository, which
// is identical for every tag, so only the manifest specifier changes.
//
// Parameters:
//   - log: Logger.
//   - c: Container whose image supplies the repository and registry host.
//   - settings: Registry request settings.
//   - hostOverride: Optional host or endpoint URL to use instead of the canonical host.
//   - tag: Exact tag to target. An empty tag yields the container's own reference.
//
// Returns:
//   - string: The final manifest URL.
//   - string: The image registry's host, before hostOverride applies.
//   - *url.URL: The parsed URL object.
//   - error: Non-nil if construction or validation fails.
func buildManifestURLForTag(log *zerolog.Logger,
	c *container.Container,
	settings registryConfig.Registry,
	hostOverride string,
	tag string,
) (string, string, *url.URL, error) {
	imageName, err := imageNameForTag(c.ImageName(), tag)
	if err != nil {
		log.Debug().
			Err(err).
			Str("image", c.ImageName()).
			Str("tag", tag).
			Msg("Failed to resolve image name for tag")

		return "", "", nil, fmt.Errorf("%w: %w", errFailedBuildManifestURL, err)
	}

	return buildManifestURLForImage(log, imageName, c.Name(), settings, hostOverride)
}

// imageNameForTag returns the image reference with its tag replaced by tag.
//
// The repository and registry host are preserved. A reference that carries no
// usable tag gets one appended, so a bare repository name still resolves.
//
// Parameters:
//   - imageName: Original image reference.
//   - tag: Exact tag to apply, or empty to leave the reference unchanged.
//
// Returns:
//   - string: Image reference for the requested tag.
//   - error: Non-nil when the reference or the tag cannot be parsed.
func imageNameForTag(imageName, tag string) (string, error) {
	if tag == "" {
		return imageName, nil
	}

	named, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	tagged, err := reference.WithTag(reference.TrimNamed(named), tag)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	return tagged.String(), nil
}

// buildManifestURLForImage constructs and validates a manifest URL for an image
// reference.
//
// Parameters:
//   - log: Logger.
//   - imageName: Image reference supplying the registry host and repository.
//   - containerName: Container name used only for log context.
//   - settings: Registry request settings.
//   - hostOverride: Optional host or endpoint URL to use instead of the canonical host.
//
// Returns:
//   - string: The final manifest URL.
//   - string: The image registry's host, before hostOverride applies.
//   - *url.URL: The parsed URL object.
//   - error: Non-nil if construction or validation fails.
func buildManifestURLForImage(log *zerolog.Logger,
	imageName,
	containerName string,
	settings registryConfig.Registry,
	hostOverride string,
) (string, string, *url.URL, error) {
	fields := map[string]any{
		"container": containerName,
		"image":     imageName,
	}

	scheme := auth.Scheme(settings)

	// The canonical URL is on the image registry's host, which already maps
	// lscr.io images to ghcr.io, where they are hosted.
	manifestURLStr, err := manifest.BuildManifestURLForImage(log, imageName, "", scheme)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to build manifest URL")

		return "", "", nil, fmt.Errorf("%w: %w", errFailedBuildManifestURL, err)
	}

	parsedURL, parseErr := url.Parse(manifestURLStr)
	if parseErr != nil {
		return "", "", nil, fmt.Errorf(
			"%w: failed to parse manifest URL: %w",
			errFailedBuildManifestURL,
			parseErr,
		)
	}

	originalHost := parsedURL.Host

	if hostOverride != "" {
		// Resolve the override as the challenge request does, so a mirror given
		// as a URL or as a bare host reaches the same host for both requests.
		host, scheme, err := auth.ResolveEndpoint(hostOverride, parsedURL.Host, parsedURL.Scheme)
		if err != nil {
			return "", "", nil, fmt.Errorf("%w: %w", errFailedBuildManifestURL, err)
		}

		parsedURL.Host = host
		parsedURL.Scheme = scheme
		manifestURLStr = parsedURL.String()
	}

	return manifestURLStr, originalHost, parsedURL, nil
}

// newDigestLookup returns a digest lookup for the registry endpoints.
//
// When a tag is supplied the manifest requests target that tag of the
// container's repository instead of the container's own reference, so a
// candidate release tag can be confirmed without pulling it. Authentication
// stays scoped to the container's repository, which every tag shares.
//
// Requests go to the endpoints in order with mirrors before the canonical
// registry host, which an empty string represents. The lookup holds one
// rate-limit slot until release is called.
//
// Parameters:
//   - log: Logger.
//   - ctx: Context for the rate-limit hold.
//   - c: Container whose digest is looked up. Callers skip a locally built
//     image, which has no repo digests for the registry to match.
//   - registryAuth: Base64-encoded auth string.
//   - settings: Registry request settings.
//   - limiter: Rate limiter shared by registry requests.
//   - tag: Optional tag override for the manifest specifier.
//   - endpoints: Registry mirror host overrides to try before the canonical
//     host. Empty means the canonical host alone.
//
// Returns:
//   - *digestLookup: The lookup.
//   - func(): Releases the rate-limit slot.
//   - error: Non-nil if the manifest URL cannot be built or the slot cannot be
//     held.
func newDigestLookup(log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	registryAuth string,
	settings registryConfig.Registry,
	limiter *ratelimit.Limiter,
	tag string,
	endpoints []string,
) (*digestLookup, func(), error) {
	fields := map[string]any{
		"container": c.Name(),
		"image":     c.ImageName(),
	}

	// Transform the provided auth string into a usable format for registry authentication.
	registryAuth = auth.TransformAuth(log, registryAuth)

	manifestURL, originalHost, _, err := buildManifestURL(log, c, settings, "")
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to build manifest URL")

		return nil, nil, fmt.Errorf("failed to build manifest URL: %w", err)
	}

	log.Debug().
		Fields(fields).
		Str("original_host", originalHost).
		Str("manifest_url", manifestURL).
		Msg("Built manifest URL for container")

	limitKey, release, err := limiter.HoldAnonymous(ctx, originalHost, registryAuth != "")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errFailedGetToken, err)
	}

	// If no endpoints specified, use a single empty endpoint (canonical host).
	if len(endpoints) == 0 {
		endpoints = []string{""}
	}

	states := make([]endpointState, len(endpoints))
	for i, endpoint := range endpoints {
		states[i].endpoint = endpoint

		if endpoint != "" {
			states[i].fields = map[string]any{
				"registry_endpoint": auth.RedactEndpoint(endpoint),
			}
		}
	}

	return &digestLookup{
		log:          log,
		c:            c,
		client:       auth.NewAuthClient(log, settings),
		limiter:      limiter,
		fields:       fields,
		registryAuth: registryAuth,
		endpoints:    states,
		tag:          tag,
		originalHost: originalHost,
		limitKey:     limitKey,
		settings:     settings,
	}, release, nil
}

// fetch returns the image's digest from the first registry endpoint whose
// response to a manifest request with method contains one.
//
// When no response contains a digest, the error is the last endpoint's. An
// earlier HEAD response that triggered the GET fallback is returned instead
// unless the last endpoint responded with 404 or refused the repository, since
// a GET request would receive the same response. A rate-limited response ends
// the lookup at once.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - method: HTTP method ("HEAD" or "GET").
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil when no response contains a digest.
func (dl *digestLookup) fetch(ctx context.Context, method string) (string, error) {
	var (
		lastErr error
		// fallbackErr is the first HEAD response that triggered the GET
		// fallback. The next endpoint's HEAD request comes first, since Docker
		// Hub does not count HEAD requests toward its pull rate limit.
		fallbackErr error
	)

	for i := range dl.endpoints {
		digest, err := dl.fetchFrom(ctx, &dl.endpoints[i], method)
		if err == nil {
			return digest, nil
		}

		if ratelimit.Is(err) {
			return "", err
		}

		lastErr = err

		if fallbackErr == nil && errors.Is(err, errHeadFallback) {
			fallbackErr = err
		}
	}

	// The canonical registry is the last endpoint. Its 404 or refusal is final
	// for the image, and a GET request would receive the same response.
	if fallbackErr == nil || errors.Is(lastErr, errManifestNotFound) || errors.Is(lastErr, errRepositoryDenied) {
		return "", lastErr
	}

	return "", fallbackErr
}

// fetchFrom returns the image's digest from one registry endpoint, using the
// outcome of earlier requests to it.
//
// The first request to an endpoint uses method. After a failed request:
//   - A 404 or a refused repository is returned again, since a new request
//     would receive the same response.
//   - A request that received no response is not repeated, since it would
//     wait for the same failure.
//   - A token request that failed otherwise is repeated and followed by a HEAD
//     request, since Docker Hub does not count HEAD requests toward its pull
//     rate limit. A request with method follows only if that HEAD response
//     triggers the GET fallback.
//   - Any other failure, such as a HEAD response that triggered the GET
//     fallback, is followed by a request with method.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - state: The endpoint to request the digest from.
//   - method: HTTP method ("HEAD" or "GET").
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil if no response from the endpoint contains a digest.
func (dl *digestLookup) fetchFrom(ctx context.Context, state *endpointState, method string) (string, error) {
	switch {
	case errors.Is(state.err, errManifestNotFound),
		errors.Is(state.err, errRepositoryDenied),
		errors.Is(state.err, auth.ErrRequestFailed):
		return "", state.err
	case errors.Is(state.err, errFailedGetToken):
		digest, err := dl.requestDigest(ctx, state, http.MethodHead)
		if method == http.MethodHead || !errors.Is(err, errHeadFallback) {
			return digest, err
		}

		return dl.requestDigest(ctx, state, method)
	default:
		return dl.requestDigest(ctx, state, method)
	}
}

// requestDigest sends one endpoint a manifest request with method and returns
// the digest in its response.
//
// Each call resolves the endpoint again through the auth package, which reuses
// a cached bearer token only until it expires. The outcome is recorded on the
// endpoint.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - state: The endpoint to send the request to.
//   - method: HTTP method ("HEAD" or "GET").
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil if the token request, the manifest URL or the manifest
//     request fails.
func (dl *digestLookup) requestDigest(ctx context.Context, state *endpointState, method string) (string, error) {
	var (
		target *manifestTarget
		digest string
	)

	target, state.err = dl.resolve(ctx, state)
	if state.err == nil {
		digest, state.err = dl.requestManifest(ctx, state, target, method)
	}

	if state.err != nil {
		return "", state.err
	}

	dl.log.Debug().
		Fields(dl.fields).
		Fields(state.fields).
		Str("remote_digest", digest).
		Msg("Fetched remote digest")

	return digest, nil
}

// resolve returns the target of one endpoint's next manifest request.
//
// The request goes to the host the endpoint's challenge redirected to. Without
// such a redirect it goes to the endpoint.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - state: The endpoint to resolve.
//
// Returns:
//   - *manifestTarget: The manifest URL, token and redirect flag.
//   - error: Non-nil if the token request or the manifest URL fails.
func (dl *digestLookup) resolve(ctx context.Context, state *endpointState) (*manifestTarget, error) {
	result, err := dl.limiter.DoValue(ctx, dl.log, dl.limitKey, func() (auth.TokenResult, error) {
		return auth.GetToken(dl.log,
			ctx,
			dl.c,
			dl.registryAuth,
			dl.client,
			dl.settings,
			state.endpoint,
		)
	})
	if err != nil {
		dl.log.Debug().
			Err(err).
			Fields(dl.fields).
			Fields(state.fields).
			Msg("Failed to get token from endpoint")

		return nil, fmt.Errorf("%w: %w", errFailedGetToken, err)
	}

	if result.Token == "" {
		dl.log.Debug().
			Fields(dl.fields).
			Fields(state.fields).
			Msg("No authentication required, proceeding with request")
	} else {
		dl.log.Debug().
			Fields(dl.fields).
			Fields(state.fields).
			Bool("redirected", result.Redirected).
			Str("redirect_host", result.RedirectHost).
			Msg("Received token and redirect flag from GetToken")
	}

	// Decide which host to use for this endpoint's manifest requests.
	// Priority:
	// 1. The host the registry's challenge redirected to
	// 2. Current mirror endpoint (when using registry mirrors)
	// 3. Canonical host (empty string)
	hostForManifest := challengeRedirectHost(result, dl.originalHost)
	if hostForManifest == "" && state.endpoint != "" {
		// Use the current registry mirror endpoint for the manifest request.
		hostForManifest = state.endpoint
	}

	manifestURL, _, parsedURL, err := buildManifestURLForTag(dl.log,
		dl.c,
		dl.settings,
		hostForManifest,
		dl.tag,
	)
	if err != nil {
		dl.log.Debug().
			Err(err).
			Fields(dl.fields).
			Fields(state.fields).
			Msg("Failed to build manifest URL")

		return nil, err
	}

	// Only the canonical registry's redirect lets a failed manifest request
	// return to the original host. A mirror's token belongs to the mirror,
	// and the canonical endpoint is tried after it with its own challenge.
	return &manifestTarget{
		parsedURL:  parsedURL,
		url:        manifestURL,
		token:      result.Token,
		redirected: result.Redirected && state.endpoint == "",
	}, nil
}

// requestManifest sends one endpoint a manifest request and returns the digest
// in its response, retrying at most once when the response requires it.
//
// A retry that follows the registry's redirect to another origin is sent
// without the token, since a redirect can point anywhere.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - state: The endpoint the request goes to.
//   - target: The endpoint's resolved manifest request.
//   - method: HTTP method ("HEAD" or "GET").
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil if the request or its retry fails.
func (dl *digestLookup) requestManifest(
	ctx context.Context,
	state *endpointState,
	target *manifestTarget,
	method string,
) (string, error) {
	dl.log.Debug().
		Fields(dl.fields).
		Fields(state.fields).
		Str("method", method).
		Str("url", target.url).
		Msg("Fetching digest")

	var reply manifestReply

	err := dl.limiter.Do(ctx, dl.log, dl.limitKey, func() error {
		var sendErr error

		reply, sendErr = dl.sendManifest(ctx, method, target)

		return sendErr
	})
	if err != nil {
		dl.log.Debug().
			Err(err).
			Fields(dl.fields).
			Fields(state.fields).
			Msg("Failed to handle manifest response")

		return "", err //nolint:wrapcheck // The request's own error, or the limiter's, already names the failure.
	}

	if reply.retryURL == "" {
		return reply.digest, nil
	}

	dl.log.Debug().
		Fields(dl.fields).
		Fields(state.fields).
		Str("retry_url", auth.RedactURL(reply.retryURL)).
		Msg("Retrying manifest request with updated URL")

	// Hosts Watchtower picks for a retry serve the same registry, so they
	// get the token.
	retryToken := target.token

	if reply.redirect && !sameOrigin(target.url, reply.retryURL) {
		dl.log.Debug().
			Fields(dl.fields).
			Fields(state.fields).
			Msg("Manifest redirect leaves the registry's origin, retrying without the token")

		retryToken = ""
	}

	retry := &manifestTarget{
		parsedURL:  target.parsedURL,
		url:        reply.retryURL,
		token:      retryToken,
		redirected: target.redirected,
	}

	digest, err := dl.retryManifest(ctx, method, retry)
	if err != nil {
		dl.log.Debug().
			Err(err).
			Fields(dl.fields).
			Fields(state.fields).
			Str("retry_url", auth.RedactURL(reply.retryURL)).
			Msg("Failed to retry manifest request")

		return "", err
	}

	return digest, nil
}

// retryManifest sends the manifest request again to the retry URL from a
// response.
//
// The request is retried only once, so a retry whose response requires yet
// another retry is an error.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - method: HTTP method ("HEAD" or "GET").
//   - retry: The retry request, with the retry URL and the token it carries.
//
// Returns:
//   - string: Normalized digest.
//   - error: Non-nil if the retry fails or requires yet another retry. A HEAD
//     retry whose response contains no digest triggers the GET fallback.
func (dl *digestLookup) retryManifest(ctx context.Context, method string, retry *manifestTarget) (string, error) {
	var digest string

	err := dl.limiter.Do(ctx, dl.log, dl.limitKey, func() error {
		reply, sendErr := dl.sendManifest(ctx, method, retry)
		if sendErr != nil {
			return sendErr
		}

		if reply.retryURL != "" {
			return fmt.Errorf("%w: %s", errUnresolvedManifestRetry, auth.RedactURL(reply.retryURL))
		}

		digest = reply.digest

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("manifest retry: %w", err)
	}

	return digest, nil
}

// sendManifest sends one manifest request and reads its response.
//
// Parameters:
//   - ctx: Context for request lifecycle control.
//   - method: HTTP method ("HEAD" or "GET").
//   - target: Where and how the request goes. Its redirect flag and parsed URL
//     determine how the response is handled.
//
// Returns:
//   - manifestReply: The digest, or the URL at which the response requires
//     the request to be retried.
//   - error: Non-nil if the request cannot be sent or its response contains
//     neither a digest nor a retry URL.
func (dl *digestLookup) sendManifest(ctx context.Context, method string, target *manifestTarget) (manifestReply, error) {
	req, err := makeManifestRequest(ctx, method, target.url, target.token)
	if err != nil {
		return manifestReply{}, err
	}

	resp, err := dl.client.Do(req)
	if err != nil {
		return manifestReply{}, err //nolint:wrapcheck // The registry client's error already names the failed request.
	}

	defer func() { _ = resp.Body.Close() }()

	return handleManifestResponse(dl.log,
		resp,
		method,
		dl.originalHost,
		target.redirected,
		target.parsedURL,
	)
}

// handleManifestResponse processes the HTTP response, handles redirects, and extracts the digest.
//
// It checks for redirects, updates the manifest URL if necessary, and extracts the digest
// from the response headers or body based on the request method.
//
// Parameters:
//   - resp: The HTTP response from the manifest request.
//   - method: The HTTP method used ("HEAD" or "GET").
//   - originalHost: The image registry's host.
//   - redirected: Whether the canonical registry's challenge redirected the request to
//     another host.
//   - parsedURL: Parsed URL of the request, whose host is the host it went to.
//
// Returns:
//   - manifestReply: The digest, or the URL at which the response requires
//     the request to be retried.
//   - error: Non-nil if processing or extraction fails, nil on success.
func handleManifestResponse(log *zerolog.Logger,
	resp *http.Response,
	method, originalHost string,
	redirected bool,
	parsedURL *url.URL,
) (manifestReply, error) {
	currentHost := parsedURL.Host

	fields := map[string]any{
		"method":        method,
		"status_code":   resp.StatusCode,
		"status":        resp.Status,
		"original_host": originalHost,
		"redirected":    redirected,
		"request_host":  resp.Request.URL.Host,
		"current_host":  currentHost,
	}

	log.Debug().
		Fields(fields).
		Msg("Handling manifest response")

	if resp.StatusCode == http.StatusTooManyRequests {
		body := ratelimit.ReadBody(resp, ratelimit.DefaultBodyLimit)

		log.Debug().
			Fields(fields).
			Msg("Registry returned 429 for manifest request")

		return manifestReply{}, ratelimit.FromResponse(resp, body)
	}

	// A refused repository would refuse a GET too.
	refused := auth.IsRepositoryRefusal(resp)

	// A failed HEAD request triggers the GET fallback. A 404 or a refused
	// repository is excluded, since a GET request would receive the same
	// response.
	headFallbackCondition := method == http.MethodHead &&
		(resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices) &&
		resp.StatusCode != http.StatusNotFound && !refused
	log.Debug().
		Fields(fields).
		Bool("head_fallback_condition", headFallbackCondition).
		Msg("Checking HEAD fallback condition")

	if headFallbackCondition {
		log.Debug().
			Fields(fields).
			Msg("HEAD request failed, asking for the GET fallback")

		return manifestReply{}, fmt.Errorf("%w: status %s", errHeadFallback, resp.Status)
	}

	// Check for redirect status codes (3xx)
	redirectCondition := resp.StatusCode >= http.StatusMultipleChoices &&
		resp.StatusCode < http.StatusBadRequest
	log.Debug().
		Fields(fields).
		Bool("redirect_condition", redirectCondition).
		Msg("Checking redirect condition")

	if redirectCondition {
		// The client follows redirects itself, so a redirect reaches here only
		// past its redirect limit. The retry goes to the whole Location,
		// resolved against the request, so its path and query are kept.
		location := resp.Header.Get("Location")
		if location != "" {
			locationURL, err := url.Parse(location)
			if err == nil {
				target := resp.Request.URL.ResolveReference(locationURL)
				if target.Host != "" && target.String() != resp.Request.URL.String() {
					// The Location can carry signed query parameters, so only its
					// host is logged.
					log.Debug().
						Fields(fields).
						Str("redirect_host", target.Host).
						Msg("Manifest request redirected, retrying at the redirect target")

					*parsedURL = *target

					return manifestReply{digest: "", retryURL: target.String(), redirect: true}, nil
				}
			}
		}
	}

	// Check for successful status code (only for GET requests, since HEAD is handled above).
	successCondition := resp.StatusCode >= http.StatusOK &&
		resp.StatusCode < http.StatusMultipleChoices
	log.Debug().
		Fields(fields).
		Bool("success_condition", successCondition).
		Msg("Checking success status condition")

	if !successCondition {
		// For HEAD requests, do not retry on 404 to avoid unnecessary GET fallback
		if method == http.MethodHead && resp.StatusCode == http.StatusNotFound {
			log.Debug().
				Fields(fields).
				Str("error", "HEAD request returned 404, not retrying").
				Msg("Response status not successful")

			return manifestReply{}, fmt.Errorf(
				"%w: %w: status %s",
				errManifestNotFound,
				errInvalidRegistryResponse,
				resp.Status,
			)
		}

		// Only a request the registry's own challenge redirected is retried. A
		// mirror's token belongs to the mirror. The next endpoint is tried anyway.
		// The caller passes redirected as false for a mirror.
		if redirected && retriesOnOriginalHost(resp.StatusCode, currentHost, originalHost) {
			log.Debug().
				Fields(fields).
				Msg("401/404 on redirected host, retrying on original host")

			parsedURL.Host = originalHost

			return manifestReply{digest: "", retryURL: parsedURL.String(), redirect: false}, nil
		}

		if refused {
			log.Debug().
				Fields(fields).
				Msg("Registry denied access to the repository")

			return manifestReply{}, fmt.Errorf("%w: status %s", errRepositoryDenied, resp.Status)
		}

		log.Debug().
			Fields(fields).
			Str("error", "invalid status code").
			Msg("Response status not successful")

		return manifestReply{}, fmt.Errorf("%w: status %s", errInvalidRegistryResponse, resp.Status)
	}

	// Extract the digest based on the request method (HEAD from headers, GET from body).
	var (
		digest string
		err    error
	)

	log.Debug().
		Fields(fields).
		Msg("Extracting digest")

	if method == http.MethodHead {
		digest, err = extractHeadDigest(log, resp)
	} else {
		digest, err = extractGetDigest(log, resp)
	}

	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to extract digest")

		return manifestReply{}, err
	}

	log.Debug().
		Fields(fields).
		Str("extracted_digest", digest).
		Msg("Successfully extracted digest")

	return manifestReply{digest: digest, retryURL: "", redirect: false}, nil
}

// retriesOnOriginalHost reports whether a manifest request that failed on the
// host the registry's challenge redirected to is retried on the image
// registry's own host.
//
// It is true for a 401 or 404 from a host other than the registry's. The
// redirect host may serve the challenge without serving the manifest. Hosts
// compare case-insensitively.
//
// Parameters:
//   - status: Status of the failed response.
//   - requestHost: Host the failed request went to.
//   - originalHost: The image registry's host.
//
// Returns:
//   - bool: True when the request is retried on originalHost.
func retriesOnOriginalHost(status int, requestHost, originalHost string) bool {
	return (status == http.StatusUnauthorized || status == http.StatusNotFound) &&
		!strings.EqualFold(requestHost, originalHost)
}

// challengeRedirectHost returns the host a manifest request goes to first
// after the registry's challenge redirected to another host.
//
// Parameters:
//   - result: The token result of the challenge.
//   - originalHost: The image registry's host.
//
// Returns:
//   - string: The redirect host. It is empty when the challenge was not
//     redirected or stayed on originalHost, compared case-insensitively.
func challengeRedirectHost(result auth.TokenResult, originalHost string) string {
	if strings.EqualFold(result.RedirectHost, originalHost) {
		return ""
	}

	return result.RedirectHost
}

// extractHeadDigest extracts the image digest from a HEAD response's headers.
//
// It retrieves the digest from the "Docker-Content-Digest" header, normalizing it for comparison,
// and validates its presence to ensure a valid response from the registry.
//
// Parameters:
//   - resp: The HTTP response from a HEAD request containing headers.
//
// A missing header, or one with no hash after its algorithm such as
// "sha256:", triggers the GET fallback, as a non-success HEAD response does.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:").
//   - error: Wraps the GET fallback error if the digest header is missing or
//     has no hash.
func extractHeadDigest(log *zerolog.Logger, resp *http.Response) (string, error) {
	// Retrieve the digest from the standard header.
	header := resp.Header.Get(ContentDigestHeader)
	if header == "" {
		// A GET response can still contain the digest, or a body to hash.
		log.Debug().
			Str("status", resp.Status).
			Str("auth_header", resp.Header.Get("WWW-Authenticate")).
			Msg("HEAD response has no digest header, triggering the GET fallback")

		return "", fmt.Errorf("%w: no digest header", errHeadFallback)
	}

	// Normalize the digest (e.g., strip "sha256:") for consistency.
	digest := NormalizeDigest(log, header)
	if digest == "" {
		log.Debug().
			Str("status", resp.Status).
			Msg("HEAD response digest has no hash, asking for the GET fallback")

		return "", fmt.Errorf("%w: digest header has no hash", errHeadFallback)
	}

	log.Debug().
		Str("digest", digest).
		Msg("Extracted digest from HEAD response")

	return digest, nil
}

// extractGetDigest extracts the image digest from a GET response's headers or body.
//
// It first tries to retrieve the digest from the "Docker-Content-Digest" header.
// If the header is missing, it falls back to the response body. A JSON body
// with a top-level "digest" field provides that digest. Without that field, a
// schema 2 index or a schema 2 image manifest with a config and layers has the
// sha256 of its exact bytes as its digest, which is how a registry computes a
// manifest's digest. Any other body is read as a plain text digest for
// non-standard registries. The fallback body is limited to
// maxManifestSize.
// When attempting JSON parsing, the Content-Type header must contain "application/json",
// "application/vnd.oci", or "application/vnd.docker".
// The digest is normalized for consistency.
//
// Parameters:
//   - log: Logger for debug output.
//   - resp: The HTTP response from a GET request containing headers and body.
//
// Returns:
//   - string: The normalized digest (e.g., "abc..." without "sha256:") if present.
//   - error: An error if the digest cannot be extracted, nil if successful.
func extractGetDigest(log *zerolog.Logger, resp *http.Response) (string, error) {
	// First, try to retrieve the digest from the standard header, normalized
	// (e.g., "sha256:" stripped) for consistency. A header with no hash after
	// its algorithm carries no digest, so the body is tried as for a missing one.
	headerDigest := NormalizeDigest(log, resp.Header.Get(ContentDigestHeader))
	if headerDigest != "" {
		log.Debug().
			Str("digest", headerDigest).
			Msg("Extracted digest from GET response header")

		return headerDigest, nil
	}

	// Without the header, the digest comes from the body. An oversized body is
	// rejected before it is parsed.
	bodyBytes, err := readManifestBody(resp.Body)
	if err != nil {
		log.Debug().
			Err(err).
			Str("status", resp.Status).
			Msg("Failed to read response body for digest extraction")

		return "", fmt.Errorf("%w: %w", errInvalidRegistryResponse, err)
	}

	bodyStr := strings.TrimSpace(string(bodyBytes))
	if bodyStr == "" {
		log.Debug().
			Str("status", resp.Status).
			Msg("Response body is empty, no digest found")

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
		var manifest manifestBody
		// Attempt to unmarshal the response body as JSON.
		jsonErr := json.Unmarshal(bodyBytes, &manifest)
		if jsonErr == nil {
			// JSON unmarshaling succeeded, check if digest field contains a value.
			// A digest field with no hash after its algorithm is as empty as a missing one.
			normalizedDigest := NormalizeDigest(log, manifest.Digest)
			if normalizedDigest != "" {
				log.Debug().
					Str("digest", normalizedDigest).
					Msg("Extracted digest from JSON manifest")

				return normalizedDigest, nil
			}
			// A schema 2 manifest's digest is the sha256 of its bytes as served.
			// Schema 1 manifests are signed, and their digest leaves the
			// signatures out, so they are not hashed here, and neither is JSON
			// that does not have the shape of a manifest or index.
			if manifest.isSchema2() {
				sum := sha256.Sum256(bodyBytes)
				computedDigest := hex.EncodeToString(sum[:])

				log.Debug().
					Str("digest", computedDigest).
					Int("body_length", len(bodyBytes)).
					Msg("Computed digest from JSON manifest body")

				return computedDigest, nil
			}

			// JSON parsed successfully, but it is not a schema 2 manifest and its
			// digest field is missing or has no hash.
			log.Debug().
				Str("status", resp.Status).
				Int("body_length", len(bodyStr)).
				Str("content_type", contentType).
				Msg("JSON manifest parsed but digest field is empty")

			return "", fmt.Errorf("%w: empty digest in JSON manifest", errInvalidRegistryResponse)
		}
		// JSON parsing failed, log metadata for debugging (avoid exposing potentially sensitive content).
		log.Debug().
			Err(jsonErr).
			Str("status", resp.Status).
			Int("body_length", len(bodyStr)).
			Str("content_type", resp.Header.Get("Content-Type")).
			Msg("Failed to parse response body as JSON manifest")
	}

	// Final fallback: Try to parse as plain text digest for non-standard registries.
	// Validate that the body looks like a digest (starts with sha256: prefix and has reasonable length).
	if !strings.HasPrefix(bodyStr, "sha256:") || len(bodyStr) < 20 {
		log.Debug().
			Str("status", resp.Status).
			Int("body_length", len(bodyStr)).
			Str("content_type", resp.Header.Get("Content-Type")).
			Msg("Response body does not appear to be a valid digest")

		return "", fmt.Errorf("%w: invalid digest format in body", errInvalidRegistryResponse)
	}

	// Normalize the digest from the plain text body.
	normalizedDigest := NormalizeDigest(log, bodyStr)
	log.Debug().
		Str("digest", normalizedDigest).
		Msg("Extracted digest from plain text body")

	return normalizedDigest, nil
}

// isSchema2 reports whether the body is a schema 2 index, which lists its
// manifests, or a schema 2 image manifest, which has a config digest and a
// list of layers.
//
// Returns:
//   - bool: True for a schema 2 index or image manifest.
func (m manifestBody) isSchema2() bool {
	if m.SchemaVersion != schema2Version {
		return false
	}

	if m.Manifests != nil {
		return true
	}

	return m.Config != nil && m.Config.Digest != "" && m.Layers != nil
}

// digestsMatch compares a list of local digests with a remote digest to determine if there's a match.
//
// It normalizes both the remote digest and each local digest, checking for equality to confirm
// whether the container's image is up-to-date with the registry's latest version.
//
// Parameters:
//   - localDigests: A slice of local digests from the container's image info (e.g., "sha256:abc...").
//   - remoteDigest: The digest fetched from the registry (e.g., "sha256:abc..." or raw hash).
//
// Returns:
//   - bool: True if any normalized local digest matches the normalized remote digest, false
//     otherwise, including when the remote digest is empty.
func digestsMatch(log *zerolog.Logger, localDigests []string, remoteDigest string) bool {
	// Normalize the remote digest once for efficiency.
	normalizedRemoteDigest := NormalizeDigest(log, remoteDigest)

	// An empty remote digest identifies no image, so it matches nothing.
	if normalizedRemoteDigest == "" {
		log.Debug().
			Strs("local_digests", localDigests).
			Msg("Remote digest is empty, so no local digest matches")

		return false
	}

	log.Debug().
		Strs("local_digests", localDigests).
		Str("remote_digest", normalizedRemoteDigest).
		Msg("Comparing digests")

	for _, digest := range localDigests {
		// Cut the digest into repo and hash parts (e.g., "repo@sha256:abc").
		repo, after, found := strings.Cut(digest, "@")

		// Skip malformed digests without @ separator.
		if !found {
			log.Debug().
				Str("digest", digest).
				Msg("Skipping malformed digest without @ separator")

			continue
		}

		// Handle case where digest starts with "@" (e.g., "@sha256:abc123")
		// This is a valid format that Docker may report in some contexts.
		if repo == "" {
			log.Debug().
				Str("digest", digest).
				Str("remote_digest", normalizedRemoteDigest).
				Msg("Local digest has empty repo prefix, comparing only digest part")
		}

		// Remove the sha256: prefix, if needed.
		normalizedLocalDigest := NormalizeDigest(log, after)

		// Return true on the first match.
		if normalizedLocalDigest == normalizedRemoteDigest {
			log.Debug().
				Str("local_digest", digest).
				Str("remote_digest", normalizedRemoteDigest).
				Msg("Found digest match")

			return true
		}
	}

	return false
}

// sameOrigin reports whether two URLs share a scheme, host name, and port,
// which decides whether a redirect may receive the registry token. A URL
// without a port uses its scheme's default port.
//
// Parameters:
//   - requestURL: URL of the request that was redirected.
//   - redirectURL: URL the redirect points to.
//
// Returns:
//   - bool: True when both parse and have the same scheme, host name, and port.
func sameOrigin(requestURL, redirectURL string) bool {
	from, err := url.Parse(requestURL)
	if err != nil {
		return false
	}

	target, err := url.Parse(redirectURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(from.Scheme, target.Scheme) &&
		strings.EqualFold(from.Hostname(), target.Hostname()) &&
		effectivePort(from) == effectivePort(target)
}

// effectivePort returns the port a URL connects to, which is its scheme's
// default port when the URL names none.
//
// Parameters:
//   - u: URL to read the port of.
//
// Returns:
//   - string: The URL's port, "443" or "80" by default for HTTPS or HTTP, or
//     empty for another scheme without a port.
func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}

	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
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

	// Accept every manifest type a tag can point to. A registry that does not
	// see a multi-platform image's list type accepted can respond with one
	// platform's manifest, whose digest does not match the list digest a pull
	// records in RepoDigests.
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
	}, ", "))
	req.Header.Set("User-Agent", meta.UserAgent)

	return req, nil
}
