package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// newMockContainer creates a mockery-generated mock Container with the given image name.
func newMockContainer(t *testing.T, imageName string) *mockTypes.MockContainer {
	t.Helper()

	container := mockTypes.NewMockContainer(t)
	container.EXPECT().Name().Return("test-container").Maybe()
	container.EXPECT().ImageName().Return(imageName).Maybe()

	return container
}

const testCreatedTimestamp = "2024-01-15T10:30:00Z"

// --- JSON helpers ---

func validManifestJSON(configDigest string) string {
	return fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
		"config": {
			"mediaType": "application/vnd.docker.container.image.v1+json",
			"digest": "%s",
			"size": 1234
		}
	}`, configDigest)
}

func validConfigJSON(created string) string {
	return fmt.Sprintf(`{"created":"%s"}`, created)
}

func configJSONWithoutCreated() string {
	return `{"architecture":"amd64","os":"linux"}`
}

func multiPlatformIndexJSON(digestCurrentPlatform, digestOther string) string {
	return fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.index.v1+json",
		"manifests": [
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest": "%s",
				"size": 500,
				"platform": {
					"architecture": "%s",
					"os": "%s"
				}
			},
			{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest": "%s",
				"size": 500,
				"platform": {
					"architecture": "arm64",
					"os": "linux"
				}
			}
		]
	}`, digestCurrentPlatform, runtime.GOARCH, runtime.GOOS, digestOther)
}

func platformManifestJSON(configDigest string) string {
	return fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": {
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest": "%s",
			"size": 789
		}
	}`, configDigest)
}

// --- TestMain ---
// Register a gomega fail handler so ghttp's VerifyRequest can properly
// report assertion failures. Without this, gomega.Expect().Should() in
// VerifyRequest silently fails and the server returns HTTP 500.

func TestMain(m *testing.M) {
	gomega.RegisterFailHandler(func(message string, _ ...int) {
		panic("gomega assertion failed: " + message)
	})

	os.Exit(m.Run())
}

// --- fetchManifestForAge tests ---
// These test the manifest fetching logic using a mock HTTP server and
// an injected auth.Client (the test server's HTTP client).

func TestFetchManifestForAge_SinglePlatform(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:abc123def456789012345678901234567890123456789012345678901234567890"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/test/manifests/latest"),
			ghttp.RespondWith(http.StatusOK, validManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}},
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/test/manifests/latest")
	require.NoError(t, err)

	got, err := fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"Bearer test-token",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "single_platform"},
	)
	require.NoError(t, err)
	assert.Equal(t, configDigest, got)
}

func TestFetchManifestForAge_MultiPlatformIndex(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:cfg1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"
	platformDigest := fmt.Sprintf("sha256:plt%s%s67890abcdef1234567890abcdef1234567890abcdef1234567890", runtime.GOOS, runtime.GOARCH)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/test/manifests/latest"),
			ghttp.RespondWith(http.StatusOK, multiPlatformIndexJSON(platformDigest, "sha256:arm64digest"),
				http.Header{"Content-Type": {"application/vnd.oci.image.index.v1+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", gomega.MatchRegexp(`/v2/test/manifests/sha256:plt.*`)),
			ghttp.RespondWith(http.StatusOK, platformManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}},
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/test/manifests/latest")
	require.NoError(t, err)

	got, err := fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"Bearer test-token",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "multi_platform"},
	)
	require.NoError(t, err)
	assert.Equal(t, configDigest, got)
}

func TestFetchManifestForAge_AuthFailure(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/test/manifests/latest"),
			ghttp.RespondWith(http.StatusUnauthorized, "unauthorized"),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/test/manifests/latest")
	require.NoError(t, err)

	_, err = fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"Bearer test-token",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "auth_failure"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errFetchManifestFailed)
}

func TestFetchManifestForAge_NotFound(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/test/manifests/latest"),
			ghttp.RespondWith(http.StatusNotFound, "not found"),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/test/manifests/latest")
	require.NoError(t, err)

	_, err = fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"Bearer test-token",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "not_found"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errFetchManifestFailed)
}

func TestFetchManifestForAge_MissingConfigDigest(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/test/manifests/latest"),
			ghttp.RespondWith(http.StatusOK, `{
				"schemaVersion": 2,
				"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
				"config": {}
			}`, http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}}),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/test/manifests/latest")
	require.NoError(t, err)

	_, err = fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"Bearer test-token",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "missing_digest"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoConfigDigest)
}

// --- fetchConfigBlob tests ---

func TestFetchConfigBlob_Success(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:abc123def456789012345678901234567890123456789012345678901234567890"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/myrepo/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusOK, validConfigJSON(testCreatedTimestamp)),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/myrepo/manifests/latest")
	require.NoError(t, err)

	body, err := fetchConfigBlob(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL,
		configDigest,
		"Bearer test-token",
		logrus.Fields{"test": "config_success"},
	)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Created)

	expected, _ := time.Parse(time.RFC3339, testCreatedTimestamp)
	assert.Equal(t, expected.UTC(), config.Created.UTC())
}

func TestFetchConfigBlob_Failure500(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:fail567890123456789012345678901234567890123456789012345678901234"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/myrepo/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusInternalServerError, "internal error"),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/myrepo/manifests/latest")
	require.NoError(t, err)

	_, err = fetchConfigBlob(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL,
		configDigest,
		"Bearer test-token",
		logrus.Fields{"test": "config_500"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errFetchConfigFailed)
}

func TestFetchConfigBlob_Redirect(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:redir789012345678901234567890123456789012345678901234567890123456"
	redirectPath := "/redirected-config"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/myrepo/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusFound, nil,
				http.Header{"Location": {server.URL() + redirectPath}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", redirectPath),
			ghttp.RespondWith(http.StatusOK, validConfigJSON(testCreatedTimestamp)),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/myrepo/manifests/latest")
	require.NoError(t, err)

	body, err := fetchConfigBlob(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL,
		configDigest,
		"Bearer test-token",
		logrus.Fields{"test": "config_redirect"},
	)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Created)

	expected, _ := time.Parse(time.RFC3339, testCreatedTimestamp)
	assert.Equal(t, expected.UTC(), config.Created.UTC())
}

// --- isIndexMediaType tests ---

func TestIsIndexMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "OCI image index",
			input:    "application/vnd.oci.image.index.v1+json",
			expected: true,
		},
		{
			name:     "Docker manifest list",
			input:    "application/vnd.docker.distribution.manifest.list.v2+json",
			expected: true,
		},
		{
			name:     "Docker v2 manifest is not an index",
			input:    "application/vnd.docker.distribution.manifest.v2+json",
			expected: false,
		},
		{
			name:     "OCI image manifest is not an index",
			input:    "application/vnd.oci.image.manifest.v1+json",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "arbitrary media type",
			input:    "application/octet-stream",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isIndexMediaType(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- buildManifestURLForContainer tests ---

func TestBuildManifestURLForContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		imageName string
		scheme    string
		want      string
		wantErr   bool
	}{
		{
			name:      "Docker Hub image with tag",
			imageName: "alpine:3.19",
			scheme:    "https",
			want:      "https://index.docker.io/v2/library/alpine/manifests/3.19",
		},
		{
			name:      "Docker Hub image with HTTPS scheme",
			imageName: "nginx:latest",
			scheme:    "https",
			want:      "https://index.docker.io/v2/library/nginx/manifests/latest",
		},
		{
			name:      "custom registry with tag",
			imageName: "ghcr.io/owner/repo:v1.0",
			scheme:    "https",
			want:      "https://ghcr.io/v2/owner/repo/manifests/v1.0",
		},
		{
			name:      "HTTP scheme",
			imageName: "alpine:latest",
			scheme:    "http",
			want:      "http://index.docker.io/v2/library/alpine/manifests/latest",
		},
		{
			name:      "private registry",
			imageName: "myregistry.io/myimage:tag",
			scheme:    "https",
			want:      "https://myregistry.io/v2/myimage/manifests/tag",
		},
		{
			name:      "invalid image name returns error",
			imageName: "INVALID UPPER",
			scheme:    "https",
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			container := newMockContainer(t, tc.imageName)

			got, err := buildManifestURLForContainer(container, tc.scheme)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- buildManifestURLForAge tests ---

func TestBuildManifestURLForAge(t *testing.T) {
	t.Parallel()

	t.Run("standard Docker Hub image", func(t *testing.T) {
		t.Parallel()

		container := newMockContainer(t, "alpine:3.19")

		gotURL, gotHost, gotParsed, err := buildManifestURLForAge(container, "")
		require.NoError(t, err)

		assert.Contains(t, gotURL, "/v2/library/alpine/manifests/3.19")
		assert.Equal(t, "index.docker.io", gotHost)
		assert.NotNil(t, gotParsed)
	})

	t.Run("host override replaces host", func(t *testing.T) {
		t.Parallel()

		container := newMockContainer(t, "alpine:3.19")

		gotURL, _, _, err := buildManifestURLForAge(container, "custom.registry.io")
		require.NoError(t, err)

		assert.Contains(t, gotURL, "custom.registry.io")
		assert.Contains(t, gotURL, "/v2/library/alpine/manifests/3.19")
	})

	t.Run("lscr.io is swapped to ghcr.io", func(t *testing.T) {
		t.Parallel()

		container := newMockContainer(t, "lscr.io/owner/image:tag")

		gotURL, gotHost, _, err := buildManifestURLForAge(container, "")
		require.NoError(t, err)

		assert.Equal(t, "lscr.io", gotHost)
		assert.Contains(t, gotURL, "ghcr.io")
		assert.Contains(t, gotURL, "/v2/owner/image/manifests/tag")
	})
}

// --- selectPlatformManifest tests ---

func TestSelectPlatformManifest_PlatformMatch(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:cfgmatch56789012345678901234567890123456789012345678901234567890123"
	platformDigest := fmt.Sprintf("sha256:pltmatch%s%s90123456789012345678901234567890123456789012", runtime.GOOS, runtime.GOARCH)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", gomega.MatchRegexp(`/v2/repo/manifests/sha256:pltmatch.*`)),
			ghttp.RespondWith(http.StatusOK, platformManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}},
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/repo/manifests/latest")
	require.NoError(t, err)

	idx := imageIndex{
		MediaType:     "application/vnd.oci.image.index.v1+json",
		SchemaVersion: 2,
		Manifests: []indexEntry{
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json",
				Digest:    platformDigest,
				Size:      500,
				Platform: platform{
					Architecture: runtime.GOARCH,
					OS:           runtime.GOOS,
				},
			},
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json",
				Digest:    "sha256:arm64digest",
				Size:      500,
				Platform: platform{
					Architecture: "arm64",
					OS:           "linux",
				},
			},
		},
	}

	got, err := selectPlatformManifest(
		context.Background(),
		server.HTTPTestServer.Client(),
		idx,
		parsedURL,
		"Bearer test-token",
		"", "",
		"",
		logrus.Fields{"test": "platform_match"},
	)
	require.NoError(t, err)
	assert.Equal(t, configDigest, got)
}

func TestSelectPlatformManifest_NoMatch(t *testing.T) {
	t.Parallel()

	parsedURL, err := url.Parse("https://registry.example.com/v2/repo/manifests/latest")
	require.NoError(t, err)

	idx := imageIndex{
		MediaType:     "application/vnd.oci.image.index.v1+json",
		SchemaVersion: 2,
		Manifests: []indexEntry{
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json",
				Digest:    "sha256:unknownplatform",
				Size:      500,
				Platform: platform{
					Architecture: "unknown_arch",
					OS:           "unknown_os",
				},
			},
		},
	}

	_, err = selectPlatformManifest(
		context.Background(),
		nil,
		idx,
		parsedURL,
		"",
		"", "",
		"",
		logrus.Fields{"test": "no_match"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoPlatformMatch)
}

func TestSelectPlatformManifest_SkipsAttestationManifests(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:cfgnoattest01abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	platformDigest := fmt.Sprintf("sha256:pltnoattest%s%s345678901234567890123456789012345678901234", runtime.GOOS, runtime.GOARCH)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", gomega.MatchRegexp(`/v2/repo/manifests/sha256:pltnoattest.*`)),
			ghttp.RespondWith(http.StatusOK, platformManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}},
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/repo/manifests/latest")
	require.NoError(t, err)

	idx := imageIndex{
		MediaType:     "application/vnd.oci.image.index.v1+json",
		SchemaVersion: 2,
		Manifests: []indexEntry{
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json",
				Digest:    "sha256:attestationdigest",
				Size:      500,
				Platform: platform{
					Architecture: runtime.GOARCH,
					OS:           runtime.GOOS,
				},
				Annotations: map[string]string{
					"vnd.docker.reference.type": "attestation-manifest",
				},
			},
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json",
				Digest:    platformDigest,
				Size:      500,
				Platform: platform{
					Architecture: runtime.GOARCH,
					OS:           runtime.GOOS,
				},
			},
		},
	}

	got, err := selectPlatformManifest(
		context.Background(),
		server.HTTPTestServer.Client(),
		idx,
		parsedURL,
		"Bearer test-token",
		"", "",
		"",
		logrus.Fields{"test": "skips_attestation"},
	)
	require.NoError(t, err)
	assert.Equal(t, configDigest, got)
}

func TestSelectPlatformManifest_MissingConfigDigest(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	platformDigest := fmt.Sprintf("sha256:pltnodgst%s%s56789012345678901234567890123456789012345678", runtime.GOOS, runtime.GOARCH)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", gomega.MatchRegexp(`/v2/repo/manifests/sha256:pltnodgst.*`)),
			ghttp.RespondWith(http.StatusOK, `{
				"schemaVersion": 2,
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"config": {}
			}`, http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}}),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/repo/manifests/latest")
	require.NoError(t, err)

	idx := imageIndex{
		MediaType:     "application/vnd.oci.image.index.v1+json",
		SchemaVersion: 2,
		Manifests: []indexEntry{
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json",
				Digest:    platformDigest,
				Size:      500,
				Platform: platform{
					Architecture: runtime.GOARCH,
					OS:           runtime.GOOS,
				},
			},
		},
	}

	_, err = selectPlatformManifest(
		context.Background(),
		server.HTTPTestServer.Client(),
		idx,
		parsedURL,
		"Bearer test-token",
		"", "",
		"",
		logrus.Fields{"test": "missing_digest"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoConfigDigest)
}

// --- End-to-end pipeline tests: manifest fetch → config fetch → parse ---

func TestPipeline_SinglePlatformManifest_ReturnsCreationTime(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:ab4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusOK, validManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusOK, validConfigJSON(testCreatedTimestamp)),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	client := server.HTTPTestServer.Client()
	ctx := context.Background()
	fields := logrus.Fields{"test": "pipeline_single"}

	digest, err := fetchManifestForAge(ctx, client, parsedURL.String(), "", parsedURL, "", "", "", fields)
	require.NoError(t, err)
	assert.Equal(t, configDigest, digest)

	body, err := fetchConfigBlob(ctx, client, parsedURL, digest, "", fields)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Created)

	expected, _ := time.Parse(time.RFC3339, testCreatedTimestamp)
	assert.Equal(t, expected.UTC(), config.Created.UTC())
}

func TestPipeline_MultiPlatformIndex_ReturnsCreationTime(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	expectedTime := "2024-02-20T14:00:00Z"
	configDigest := "sha256:configdef4567890abcdef1234567890abcdef1234567890abcdef1234567890ab"
	platformDigest := fmt.Sprintf("sha256:platform%s%s7890abcdef1234567890abcdef1234567890abcdef12345678", runtime.GOOS, runtime.GOARCH)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusOK, multiPlatformIndexJSON(platformDigest, "sha256:arm64digest"),
				http.Header{"Content-Type": {"application/vnd.oci.image.index.v1+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", gomega.MatchRegexp(`/v2/library/alpine/manifests/sha256:platform.*`)),
			ghttp.RespondWith(http.StatusOK, platformManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusOK, validConfigJSON(expectedTime)),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	client := server.HTTPTestServer.Client()
	ctx := context.Background()
	fields := logrus.Fields{"test": "pipeline_multi"}

	digest, err := fetchManifestForAge(ctx, client, parsedURL.String(), "", parsedURL, "", "", "", fields)
	require.NoError(t, err)
	assert.Equal(t, configDigest, digest)

	body, err := fetchConfigBlob(ctx, client, parsedURL, digest, "", fields)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Created)

	expected, _ := time.Parse(time.RFC3339, expectedTime)
	assert.Equal(t, expected.UTC(), config.Created.UTC())
}

func TestPipeline_MissingCreationTimestamp(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:confignocr890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusOK, validManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusOK, configJSONWithoutCreated()),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	client := server.HTTPTestServer.Client()
	ctx := context.Background()
	fields := logrus.Fields{"test": "pipeline_missing_created"}

	digest, err := fetchManifestForAge(ctx, client, parsedURL.String(), "", parsedURL, "", "", "", fields)
	require.NoError(t, err)

	body, err := fetchConfigBlob(ctx, client, parsedURL, digest, "", fields)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	require.NoError(t, err)
	assert.Nil(t, config.Created, "created field should be nil when absent")
}

func TestPipeline_ConfigBlobNotFound(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:configfail01abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusOK, validManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusInternalServerError,
				`{"errors":[{"code":"UNKNOWN","message":"internal error"}]}`,
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	client := server.HTTPTestServer.Client()
	ctx := context.Background()
	fields := logrus.Fields{"test": "pipeline_config_fail"}

	digest, err := fetchManifestForAge(ctx, client, parsedURL.String(), "", parsedURL, "", "", "", fields)
	require.NoError(t, err)

	_, err = fetchConfigBlob(ctx, client, parsedURL, digest, "", fields)
	require.Error(t, err)
	assert.ErrorIs(t, err, errFetchConfigFailed)
}

func TestPipeline_InvalidConfigJSON(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	configDigest := "sha256:configinval01abcdef1234567890abcdef1234567890abcdef1234567890abcd"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusOK, validManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusOK, "not valid json"),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	client := server.HTTPTestServer.Client()
	ctx := context.Background()
	fields := logrus.Fields{"test": "pipeline_invalid_json"}

	digest, err := fetchManifestForAge(ctx, client, parsedURL.String(), "", parsedURL, "", "", "", fields)
	require.NoError(t, err)

	body, err := fetchConfigBlob(ctx, client, parsedURL, digest, "", fields)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	assert.Error(t, err, "expected JSON decode error for malformed config")
}

func TestPipeline_RedirectOnBlob(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	expectedTime := "2024-03-10T08:00:00Z"
	configDigest := "sha256:configredir01abcdef1234567890abcdef1234567890abcdef1234567890abcde"
	redirectPath := "/redirected-blob"

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusOK, validManifestJSON(configDigest),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/blobs/"+configDigest),
			ghttp.RespondWith(http.StatusFound, nil,
				http.Header{"Location": {server.URL() + redirectPath}},
			),
		),
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", redirectPath),
			ghttp.RespondWith(http.StatusOK, validConfigJSON(expectedTime)),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	client := server.HTTPTestServer.Client()
	ctx := context.Background()
	fields := logrus.Fields{"test": "pipeline_redirect"}

	digest, err := fetchManifestForAge(ctx, client, parsedURL.String(), "", parsedURL, "", "", "", fields)
	require.NoError(t, err)

	body, err := fetchConfigBlob(ctx, client, parsedURL, digest, "", fields)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	var config imageConfig

	err = json.NewDecoder(body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Created)

	expected, _ := time.Parse(time.RFC3339, expectedTime)
	assert.Equal(t, expected.UTC(), config.Created.UTC())
}

func TestPipeline_ManifestNotFound(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusNotFound,
				`{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`,
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	_, err = fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "pipeline_not_found"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errFetchManifestFailed)
}

func TestPipeline_AuthFailure(t *testing.T) {
	t.Parallel()

	server := ghttp.NewServer()
	t.Cleanup(server.Close)

	server.AppendHandlers(
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v2/library/alpine/manifests/3.19"),
			ghttp.RespondWith(http.StatusUnauthorized,
				`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`,
			),
		),
	)

	parsedURL, err := url.Parse(server.URL() + "/v2/library/alpine/manifests/3.19")
	require.NoError(t, err)

	_, err = fetchManifestForAge(
		context.Background(),
		server.HTTPTestServer.Client(),
		parsedURL.String(),
		"Bearer invalid",
		parsedURL,
		"", "",
		"",
		logrus.Fields{"test": "pipeline_auth_fail"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errFetchManifestFailed)
}
