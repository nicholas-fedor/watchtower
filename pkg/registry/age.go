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
// For cross-platform monitoring, set WATCHTOWER_REGISTRY_PLATFORM_OS and
// WATCHTOWER_REGISTRY_PLATFORM_ARCH to override the runtime defaults.
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
	targetOS := viper.GetString("WATCHTOWER_REGISTRY_PLATFORM_OS")
	targetArch := viper.GetString("WATCHTOWER_REGISTRY_PLATFORM_ARCH")

	// Transform auth credentials into usable format.
	registryAuth = auth.TransformAuth(registryAuth)

	// Use the cached HTTP client for registry requests.
	client := auth.NewAuthClient()

	// Obtain an authentication token for the registry.
	token, challengeHost, redirected, err := auth.GetToken(
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

	// Build the manifest URL.
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

	// If redirected during auth, use the challenge host for manifest requests.
	if challengeHost != "" && challengeHost != originalHost && redirected {
		manifestURL, _, parsedURL, err = buildManifestURLForAge(
			container,
			challengeHost,
		)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to build manifest URL with challenge host")

			return time.Time{},
				fmt.Errorf("%w: %w", errFetchManifestFailed, err)
		}
	}

	// Fetch the manifest.
	configDigest, err := fetchManifestForAge(
		ctx,
		client,
		manifestURL,
		token,
		parsedURL,
		targetOS,
		targetArch,
		fields,
	)
	if err != nil {
		return time.Time{}, err
	}

	logrus.WithFields(fields).
		WithField("config_digest", configDigest).
		Debug("Extracted config digest from manifest")

	// Fetch the config blob.
	configBody, err := fetchConfigBlob(
		ctx,
		client,
		parsedURL,
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

// fetchManifestForAge fetches the image manifest and extracts the config digest.
// It handles multi-platform indexes by selecting the platform-specific manifest.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - manifestURL: URL of the manifest to fetch.
//   - token: Authentication token.
//   - parsedURL: Parsed manifest URL.
//   - targetOS: Target OS for platform selection (empty for runtime.GOOS).
//   - targetArch: Target architecture for platform selection (empty for runtime.GOARCH).
//   - fields: Logging fields.
//
// Returns:
//   - string: The config digest.
//   - error: Non-nil if fetching fails.
func fetchManifestForAge(
	ctx context.Context,
	client auth.Client,
	manifestURL, token string,
	parsedURL *url.URL,
	targetOS, targetArch string,
	fields logrus.Fields,
) (string, error) {
	// Create the manifest request with broad Accept headers.
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		manifestURL,
		nil,
	)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to create manifest request")

		return "",
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

		return "",
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}
	defer resp.Body.Close()

	// Handle non-success responses.
	if resp.StatusCode != http.StatusOK {
		logrus.WithFields(fields).
			WithField("status", resp.StatusCode).
			Debug("Manifest request returned non-OK status")

		return "",
			fmt.Errorf(
				"%w: status %d",
				errFetchManifestFailed,
				resp.StatusCode,
			)
	}

	// Read the manifest body with a size limit to prevent unbounded memory allocation.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to read manifest response")

		return "",
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	if len(body) > maxManifestSize {
		logrus.WithFields(fields).
			WithField("size", len(body)).
			WithField("limit", maxManifestSize).
			Debug("Manifest response exceeds size limit")

		return "",
			fmt.Errorf(
				"%w: %d bytes exceeds limit of %d bytes",
				errManifestTooLarge,
				len(body),
				maxManifestSize,
			)
	}

	// Try to parse as image index (multi-platform).
	var index imageIndex

	idxErr := json.Unmarshal(body, &index)
	if idxErr == nil && isIndexMediaType(index.MediaType) {
		return selectPlatformManifest(
			ctx,
			client,
			index,
			parsedURL,
			token,
			targetOS,
			targetArch,
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
			fmt.Errorf("%w: %w", errFetchManifestFailed, err)
	}

	if manifest.Config.Digest == "" {
		logrus.WithFields(fields).
			Debug("Manifest does not contain config digest")

		return "", errNoConfigDigest
	}

	return manifest.Config.Digest, nil
}

// selectPlatformManifest selects the platform-specific manifest from an image index
// and fetches it to extract the config digest.
//
// If targetOS or targetArch are empty, runtime.GOOS and runtime.GOARCH are used as defaults,
// allowing cross-platform monitoring via environment variables or configuration overrides.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: HTTP client for registry requests.
//   - index: The parsed image index.
//   - parsedURL: Parsed URL for constructing platform manifest URLs.
//   - token: Authentication token.
//   - targetOS: Target OS for platform selection (empty for runtime.GOOS).
//   - targetArch: Target architecture for platform selection (empty for runtime.GOARCH).
//   - fields: Logging fields.
//
// Returns:
//   - string: The config digest from the platform-specific manifest.
//   - error: Non-nil if selection or fetching fails.
func selectPlatformManifest(
	ctx context.Context,
	client auth.Client,
	index imageIndex,
	parsedURL *url.URL,
	token string,
	targetOS, targetArch string,
	fields logrus.Fields,
) (string, error) {
	// Use runtime defaults if no override is specified.
	if targetOS == "" {
		targetOS = runtime.GOOS
	}

	if targetArch == "" {
		targetArch = runtime.GOARCH
	}

	// Select the matching platform entry.
	var selectedDigest string

	for _, entry := range index.Manifests {
		// Skip attestation manifests.
		if entry.Annotations != nil {
			if refType, ok := entry.Annotations["vnd.docker.reference.type"]; ok && refType == "attestation-manifest" {
				continue
			}
		}

		if entry.Platform.OS == targetOS && entry.Platform.Architecture == targetArch {
			selectedDigest = entry.Digest

			break
		}
	}

	if selectedDigest == "" {
		logrus.WithFields(fields).
			WithField("os", targetOS).
			WithField("arch", targetArch).
			Debug("No matching platform found in image index")

		return "", errNoPlatformMatch
	}

	logrus.WithFields(fields).
		WithField("digest", selectedDigest).
		Debug("Selected platform manifest from index")

	// Build URL for platform-specific manifest.
	platformURL := *parsedURL
	idx := strings.Index(platformURL.Path, "/manifests/")

	if idx == -1 {
		logrus.WithFields(fields).
			WithField("path", platformURL.Path).
			Debug("Could not find /manifests/ in URL path")

		return "",
			fmt.Errorf(
				"%w: malformed manifest URL path %q",
				errFetchManifestFailed,
				platformURL.Path,
			)
	}

	platformURL.Path = platformURL.Path[:idx+len("/manifests/")] + selectedDigest

	// Fetch the platform-specific manifest.
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		platformURL.String(),
		nil,
	)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to create platform manifest request")

		return "",
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
			fmt.Errorf(
				"%w: %w",
				errFetchManifestFailed,
				err,
			)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.WithFields(fields).
			WithField(
				"status",
				resp.StatusCode,
			).Debug("Platform manifest request returned non-OK status")

		return "",
			fmt.Errorf(
				"%w: status %d",
				errFetchManifestFailed,
				resp.StatusCode,
			)
	}

	var manifest imageManifest

	err = json.NewDecoder(resp.Body).Decode(&manifest)
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to parse platform manifest JSON")

		return "",
			fmt.Errorf(
				"%w: %w",
				errFetchManifestFailed,
				err,
			)
	}

	if manifest.Config.Digest == "" {
		logrus.WithFields(fields).
			Debug("Platform manifest does not contain config digest")

		return "", errNoConfigDigest
	}

	return manifest.Config.Digest, nil
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
	// Find /manifests/ and replace with /blobs/
	before, _, ok := strings.Cut(path, "/manifests/")
	if !ok {
		logrus.WithFields(fields).
			Debug("Could not parse image path from manifest URL")

		return nil, errFetchConfigFailed
	}

	imagePath := before
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

		redirectReq, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			location,
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
