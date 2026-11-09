package digest

import (
	"cmp"
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
	"testing"
	"testing/iotest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/registry/auth"
	mockAuth "github.com/nicholas-fedor/watchtower/internal/registry/auth/mocks"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/httpstub"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// newNopLogger returns a logger that discards every event, for benchmarks and
// fuzz targets.
//
// Returns:
//   - *zerolog.Logger: Logger that discards all events.
func newNopLogger() *zerolog.Logger {
	return new(zerolog.Nop())
}

// testDigestHash is the manifest digest the stub registries serve.
const testDigestHash = "sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"

// testRepoDigest is the repo digest of the image the stub registry containers run.
const testRepoDigest = "ghcr.io/k6io/operator@" + testDigestHash

// testLocalDigest is the digest of the image the local-only tests run, which
// is also its image ID.
const testLocalDigest = "sha256:80f07677bee57274a48929d0688bf0cfabe5e83f06f2f152dab4076445d6ab35"

// testIndexBody is an OCI image index without a top-level digest field.
const testIndexBody = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[` +
	`{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` +
	`1111111111111111111111111111111111111111111111111111111111111111","size":512,` +
	`"platform":{"architecture":"amd64","os":"linux"}}]}`

// testImageManifestBody is an OCI image manifest without a top-level digest field.
const testImageManifestBody = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
	`"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` +
	`4444444444444444444444444444444444444444444444444444444444444444","size":1024},` +
	`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:` +
	`5555555555555555555555555555555555555555555555555555555555555555","size":2048}]}`

// testManifestListBody is a Docker manifest list as a registry serves it, with
// indentation and a trailing newline that are part of the hashed bytes.
const testManifestListBody = `{
   "schemaVersion": 2,
   "mediaType": "application/vnd.docker.distribution.manifest.list.v2+json",
   "manifests": [
      {
         "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
         "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
         "size": 527,
         "platform": {"architecture": "arm64", "os": "linux"}
      }
   ]
}
`

// newRegistryContainer returns a container that runs image, whose image has
// repoDigests as its repo digests.
//
// Parameters:
//   - image: Image reference the container runs.
//   - repoDigests: The image's repo digests.
//
// Returns:
//   - types.Container: The container.
func newRegistryContainer(image string, repoDigests ...string) types.Container {
	return modelfixture.Container(
		containerfixture.WithImageName(image),
		containerfixture.WithRepoDigests(repoDigests),
	)
}

// bearerChallengeHandler returns the handler of a registry that answers /v2/
// with a bearer challenge whose realm is the /token endpoint on realmAddr.
//
// Parameters:
//   - t: Test that receives verification failures.
//   - realmAddr: Host and port of the registry that issues tokens.
//   - scope: Repository scope named in the challenge.
//
// Returns:
//   - http.HandlerFunc: Handler for the /v2/ request.
func bearerChallengeHandler(t *testing.T, realmAddr, scope string) http.HandlerFunc {
	t.Helper()

	challenge := fmt.Sprintf(`Bearer realm="http://%s/token",service="test-service",scope="%s"`, realmAddr, scope)

	return httpstub.CombineHandlers(
		httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
		httpstub.RespondWith(http.StatusUnauthorized, "", http.Header{"WWW-Authenticate": {challenge}}),
	)
}

// challengeRedirectHandler returns the handler of a registry whose /v2/
// challenge redirects to another host's /v2/.
//
// Parameters:
//   - t: Test that receives verification failures.
//   - targetAddr: Host and port the challenge redirects to.
//
// Returns:
//   - http.HandlerFunc: Handler for the /v2/ request.
func challengeRedirectHandler(t *testing.T, targetAddr string) http.HandlerFunc {
	t.Helper()

	return httpstub.CombineHandlers(
		httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
		httpstub.RespondWith(http.StatusFound, "", http.Header{
			"Location": {fmt.Sprintf("http://%s/v2/", targetAddr)},
		}),
	)
}

// tokenHandler returns the handler of a registry's /token endpoint, which
// issues a bearer token.
//
// Parameters:
//   - t: Test that receives verification failures.
//
// Returns:
//   - http.HandlerFunc: Handler for the token request.
func tokenHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return httpstub.CombineHandlers(
		httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/token")),
		httpstub.RespondWith(http.StatusOK, `{"token": "mock-token"}`, nil),
	)
}

// bearerChallengeHandlers returns the handlers of a registry that answers /v2/
// with a bearer challenge whose realm is its own /token endpoint, and then
// issues a token.
//
// Parameters:
//   - t: Test that receives verification failures.
//   - addr: Host and port of the registry.
//   - scope: Repository scope named in the challenge.
//
// Returns:
//   - []http.HandlerFunc: Challenge and token handlers, in request order.
func bearerChallengeHandlers(t *testing.T, addr, scope string) []http.HandlerFunc {
	t.Helper()

	return []http.HandlerFunc{bearerChallengeHandler(t, addr, scope), tokenHandler(t)}
}

// anonymousRegistryHandler returns the handler of a registry that answers /v2/
// without asking for authentication.
//
// Parameters:
//   - t: Test that receives verification failures.
//
// Returns:
//   - http.HandlerFunc: Handler for the /v2/ request.
func anonymousRegistryHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return httpstub.CombineHandlers(
		httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
		httpstub.RespondWith(http.StatusOK, "", nil),
	)
}

// manifestHandler returns a handler that verifies a manifest request and
// answers it with status and header and no body.
//
// Parameters:
//   - t: Test that receives verification failures.
//   - method: Expected request method.
//   - path: Expected manifest path.
//   - status: Response status code.
//   - header: Response headers, or nil for none.
//
// Returns:
//   - http.HandlerFunc: Handler for one manifest request.
func manifestHandler(t *testing.T, method, path string, status int, header http.Header) http.HandlerFunc {
	t.Helper()

	return httpstub.CombineHandlers(
		httpstub.VerifyRequest(t, method, httpstub.Path(path)),
		httpstub.RespondWith(status, "", header),
	)
}

// digestHeader returns response headers that carry digest as the manifest digest.
//
// Parameters:
//   - digest: Manifest digest to report.
//
// Returns:
//   - http.Header: Headers with the Docker-Content-Digest header set.
func digestHeader(digest string) http.Header {
	return http.Header{ContentDigestHeader: {digest}}
}

// sha256Hex returns the hex sha256 of body, the digest a registry gives a
// manifest served as body.
//
// Parameters:
//   - body: Manifest bytes.
//
// Returns:
//   - string: Hex digest without the "sha256:" prefix.
func sha256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))

	return hex.EncodeToString(sum[:])
}

// newLocalOnlyContainer returns a container of a locally built image whose
// repo digest names no registry, as Docker records for an image built and
// tagged on the host.
//
// Its configured image has no tag, and the container reports it as
// atlas-badgerdb:latest.
//
// Parameters:
//   - imageID: ID of the container's image.
//   - updates: Changes applied after the defaults, such as another image name.
//
// Returns:
//   - types.Container: The container.
func newLocalOnlyContainer(imageID string, updates ...containerfixture.Update) types.Container {
	return modelfixture.Container(append([]containerfixture.Update{
		containerfixture.WithName("atlas-badgerdb-1"),
		containerfixture.WithImageName("atlas-badgerdb"),
		containerfixture.WithImageID(imageID),
		containerfixture.WithRepoDigests([]string{
			"atlas-badgerdb@" + testLocalDigest,
		}),
	}, updates...)...)
}

// unauthorizedChallenge returns the headers of a 401 manifest response whose
// Bearer challenge reports errorCode, as Docker Hub sends with
// insufficient_scope for a repository it does not have and invalid_token for
// a token it does not accept.
//
// Parameters:
//   - errorCode: RFC 6750 error code of the challenge.
//
// Returns:
//   - http.Header: Headers holding the WWW-Authenticate challenge.
func unauthorizedChallenge(errorCode string) http.Header {
	header := http.Header{}
	header.Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",error=%q`, errorCode,
	))

	return header
}

// refusedProbeHandlers returns the handlers of an anonymous registry that
// refuses a manifest HEAD request with insufficient_scope, as Docker Hub does
// for a repository it does not have.
//
// Parameters:
//   - t: Test that receives verification failures.
//   - path: Expected manifest path.
//
// Returns:
//   - []http.HandlerFunc: Handlers in request order.
func refusedProbeHandlers(t *testing.T, path string) []http.HandlerFunc {
	t.Helper()

	return []http.HandlerFunc{
		anonymousRegistryHandler(t),
		manifestHandler(t, http.MethodHead, path, http.StatusUnauthorized, unauthorizedChallenge("insufficient_scope")),
	}
}

// clearLocalOnlyImageCache removes all cached local-only image entries, so
// each test starts without remembered local-only images.
func clearLocalOnlyImageCache() {
	localOnlyImageCache.Range(func(key, _ any) bool {
		localOnlyImageCache.Delete(key)

		return true
	})
}

// unauthorizedManifestClient returns a registry client that answers every
// manifest request with 401.
func unauthorizedManifestClient(t *testing.T) *mockAuth.MockClient {
	t.Helper()

	mc := mockAuth.NewMockClient(t)
	mc.EXPECT().Do(mock.Anything).Return(&http.Response{
		StatusCode: http.StatusUnauthorized,
		Status:     "401 Unauthorized",
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
	}, nil)

	return mc
}

// mustParseURL parses raw and panics when it is not a valid URL.
//
// Parameters:
//   - raw: URL to parse.
//
// Returns:
//   - *url.URL: Parsed URL.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("failed to parse URL %q: %v", raw, err))
	}

	return u
}

// TestNormalizeDigest verifies that the sha256: prefix is stripped and any other
// digest is returned unchanged.
func TestNormalizeDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		digest   string
		expected string
	}{
		{
			name:     "strips sha256 prefix",
			digest:   "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name:     "returns unchanged when no prefix matches",
			digest:   "md5:abc123",
			expected: "md5:abc123",
		},
		{
			name:     "empty string returns empty",
			digest:   "",
			expected: "",
		},
		{
			name:     "sha512 prefix is not stripped",
			digest:   "sha512:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12",
			expected: "sha512:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12",
		},
		{
			name:     "sha256 prefix with short value",
			digest:   "sha256:abc",
			expected: "abc",
		},
		{
			name:     "sha256 colon only",
			digest:   "sha256:",
			expected: "",
		},
		{
			name:     "digest without prefix is unchanged",
			digest:   "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547",
			expected: "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NormalizeDigest(newTestLogger(t), tt.digest)
			assert.Equal(t, tt.expected, result, "NormalizeDigest(%q)", tt.digest)
		})
	}
}

// TestDigestsMatch verifies matching a remote digest against local repo
// digests, including prefixes, multiple digests, and malformed entries.
func TestDigestsMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		localDigests []string
		remoteDigest string
		expected     bool
	}{
		{
			name:         "matching digest with sha256 prefix on remote",
			localDigests: []string{"repo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			remoteDigest: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     true,
		},
		{
			name:         "matching digest with sha256 prefix on local only",
			localDigests: []string{"repo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			remoteDigest: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     true,
		},
		{
			name:         "matching digest with sha256 prefix on both",
			localDigests: []string{"repo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			remoteDigest: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     true,
		},
		{
			name:         "non-matching digests",
			localDigests: []string{"repo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			remoteDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			expected:     false,
		},
		{
			name:         "empty local digests returns false",
			localDigests: []string{},
			remoteDigest: "sha256:abc",
			expected:     false,
		},
		{
			name:         "nil local digests returns false",
			localDigests: nil,
			remoteDigest: "sha256:abc",
			expected:     false,
		},
		{
			name: "multiple local digests with one match",
			localDigests: []string{
				"repo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"repo@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"repo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
			remoteDigest: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     true,
		},
		{
			name:         "digest with empty repo prefix",
			localDigests: []string{"@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			remoteDigest: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     true,
		},
		{
			name:         "skips malformed digest without @ separator",
			localDigests: []string{"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
			remoteDigest: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     false,
		},
		{
			name: "mixed malformed and valid digests",
			localDigests: []string{
				"sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				"repo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
			remoteDigest: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected:     true,
		},
		{
			name:         "empty remote digest",
			localDigests: []string{"repo@sha256:abc"},
			remoteDigest: "",
			expected:     false,
		},
		{
			name:         "skips local digest with nothing after @",
			localDigests: []string{"repo@"},
			remoteDigest: testDigestHash,
			expected:     false,
		},
		{
			name:         "does not match a local digest without the remote hash",
			localDigests: []string{"repo@singlepart"},
			remoteDigest: testDigestHash,
			expected:     false,
		},
		{
			name:         "does not match a local digest with multiple @ separators",
			localDigests: []string{"repo@namespace@" + testDigestHash},
			remoteDigest: testDigestHash,
			expected:     false,
		},
		{
			name:         "multiple local digests with no match",
			localDigests: []string{"repo1@sha256:hash1", "repo2@sha256:hash2"},
			remoteDigest: "sha256:hash3",
			expected:     false,
		},
		{
			name:         "empty remote digest does not match a local digest without a hash",
			localDigests: []string{"repo@sha256:"},
			remoteDigest: "sha256:",
			expected:     false,
		},
		{
			name:         "empty remote digest does not match a local digest with nothing after @",
			localDigests: []string{"repo@"},
			remoteDigest: "",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := digestsMatch(newTestLogger(t), tt.localDigests, tt.remoteDigest)
			assert.Equal(t, tt.expected, result, "digestsMatch(%v, %q)", tt.localDigests, tt.remoteDigest)
		})
	}
}

// TestFormatDigest verifies that a bare hash gains the sha256: prefix and a
// prefixed digest is kept as it is.
func TestFormatDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "already prefixed", input: "sha256:abc", want: "sha256:abc"},
		{name: "raw hash", input: "abcdef", want: "sha256:abcdef"},
		{name: "other algorithm preserved", input: "sha512:deadbeef", want: "sha512:deadbeef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, formatDigest(tt.input))
		})
	}
}

// TestCompareDigest verifies digest comparison for containers without image
// info, locally built images, and anonymous registries that answer the HEAD
// request or only a GET request.
func TestCompareDigest(t *testing.T) {
	t.Parallel()

	const (
		manifestPath = "/v2/myapp/manifests/latest"
		localHash    = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	)

	tests := []struct {
		name string
		// container is the container to compare.
		container types.Container
		// handlers returns the registry's handlers in request order.
		handlers func(t *testing.T) []http.HandlerFunc
		expected bool
		// wantErr is the expected error, nil when no error is expected.
		wantErr error
	}{
		{
			name:      "returns error when container has no image info",
			container: modelfixture.ContainerWithoutImageInfo(containerfixture.WithImageName("nginx:latest")),
			expected:  false,
			wantErr:   errMissingImageInfo,
		},
		{
			name:      "returns true for locally built image with empty RepoDigests",
			container: newRegistryContainer("local-image:latest"),
			expected:  true,
		},
		{
			name:      "matching digests via HEAD request",
			container: newRegistryContainer("registry.example.com/myapp:latest", "registry.example.com/myapp@"+localHash),
			handlers: func(t *testing.T) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusOK, digestHeader(localHash)),
				}
			},
			expected: true,
		},
		{
			name: "non-matching digests via HEAD request",
			container: newRegistryContainer("registry.example.com/myapp:latest",
				"registry.example.com/myapp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			handlers: func(t *testing.T) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusOK,
						digestHeader("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")),
				}
			},
			expected: false,
		},
		{
			name:      "falls back to GET when HEAD returns non-2xx without 404",
			container: newRegistryContainer("registry.example.com/myapp:latest", "registry.example.com/myapp@"+localHash),
			handlers: func(t *testing.T) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusInternalServerError, nil),
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodGet, manifestPath, http.StatusOK, digestHeader(localHash)),
				}
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httpstub.New(t)
			if tt.handlers != nil {
				server.AppendHandlers(tt.handlers(t)...)
			}

			got, err := CompareDigest(newTestLogger(t), context.Background(),
				tt.container, "", registryConfig.Registry{}, ratelimit.New(), server.URL())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestCompareDigest_StubRegistry verifies digest comparison against a plain HTTP
// registry, from the /v2/ challenge through the manifest HEAD request.
func TestCompareDigest_StubRegistry(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	tests := []struct {
		name string
		// handlers returns the registry's handlers in request order.
		handlers func(t *testing.T, addr string) []http.HandlerFunc
		want     bool
		// errContains is the expected error text, empty when no error is expected.
		errContains string
		// wantRequests is the number of requests the registry must receive,
		// or zero when the count depends on transport retries.
		wantRequests int
	}{
		{
			name: "should return true when HEAD request succeeds with matching digest",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
					),
				)
			},
			want:         true,
			wantRequests: 3,
		},
		{
			name: "should send the Watchtower user agent on the HEAD request",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
						httpstub.VerifyHeader(t, http.Header{"User-Agent": {meta.UserAgent}}),
						httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
					),
				)
			},
			want:         true,
			wantRequests: 3,
		},
		{
			name: "should match an image pulled from a Docker manifest list",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				// Like the reference registry, the stub answers a request that
				// does not accept Docker manifest lists with the manifest of
				// one platform, whose digest differs from the list's.
				const platformDigest = "sha256:f9086bfda061100361fc2bacf069585678d760d705cf390918ccdbda8a00980b"

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
						func(w http.ResponseWriter, r *http.Request) {
							digest := platformDigest
							if strings.Contains(r.Header.Get("Accept"), "application/vnd.docker.distribution.manifest.list.v2+json") {
								digest = testDigestHash
							}

							w.Header().Set(ContentDigestHeader, digest)
							w.WriteHeader(http.StatusOK)
						},
					),
				)
			},
			want:         true,
			wantRequests: 3,
		},
		{
			name: "should not fall back to GET when HEAD returns 404",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusNotFound, "", nil),
					),
				)
			},
			errContains:  errInvalidRegistryResponse.Error(),
			wantRequests: 3,
		},
		{
			name: "should return an error if HEAD request fails",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				dropConnection := httpstub.CombineHandlers(
					httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
					func(w http.ResponseWriter, _ *http.Request) {
						conn, _, err := http.NewResponseController(w).Hijack()
						if assert.NoError(t, err) {
							conn.Close()
						}
					},
				)

				// The HTTP transport retries a HEAD request once when the
				// kept-alive connection it reused fails, so both attempts drop.
				// A GET fallback would be unhandled and fail the test.
				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					dropConnection, dropConnection)
			},
			errContains: "EOF",
		},
		{
			name: "should handle malformed WWW-Authenticate header",
			handlers: func(t *testing.T, _ string) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
						httpstub.RespondWith(http.StatusUnauthorized, "", http.Header{
							"WWW-Authenticate": {`Bearer realm="invalid"`},
						}),
					),
				}
			},
			errContains:  "challenge header did not include all values needed to construct an auth url",
			wantRequests: 1,
		},
		{
			name: "should fall back to GET when the HEAD digest header has no hash",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					bearerChallengeHandler(t, addr, "repository:test/image:pull"),
					tokenHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusOK, digestHeader("sha256:")),
					bearerChallengeHandler(t, addr, "repository:test/image:pull"),
					manifestHandler(t, http.MethodGet, manifestPath, http.StatusOK, digestHeader(testDigestHash)),
				}
			},
			want:         true,
			wantRequests: 5,
		},
		{
			name: "should return an error when neither HEAD nor GET digest header has a hash",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					bearerChallengeHandler(t, addr, "repository:test/image:pull"),
					tokenHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusOK, digestHeader("sha256:")),
					bearerChallengeHandler(t, addr, "repository:test/image:pull"),
					manifestHandler(t, http.MethodGet, manifestPath, http.StatusOK, digestHeader("sha256:")),
				}
			},
			errContains:  "missing digest header and empty body",
			wantRequests: 5,
		},
		{
			name: "should fetch digest from HTTP registry with TLS skip",
			handlers: func(t *testing.T, _ string) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
					),
				}
			},
			want:         true,
			wantRequests: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httpstub.New(t)
			server.AppendHandlers(tt.handlers(t, server.Addr())...)

			container := newRegistryContainer(server.Addr()+"/test/image:latest", testRepoDigest)

			got, err := CompareDigest(newTestLogger(t), context.Background(),
				container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, got)

			if tt.wantRequests > 0 {
				assert.Len(t, server.ReceivedRequests(), tt.wantRequests)
			}
		})
	}
}

// TestCompareDigest_HashesManifestWithoutDigestHeader verifies that when a
// registry sends no digest header, the comparison uses the sha256 of the
// manifest list it serves, so an image pulled from that list matches.
func TestCompareDigest_HashesManifestWithoutDigestHeader(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	server := httpstub.New(t)
	server.AppendHandlers(
		bearerChallengeHandler(t, server.Addr(), "repository:test/image:pull"),
		tokenHandler(t),
		manifestHandler(t, http.MethodHead, manifestPath, http.StatusOK, digestHeader("sha256:")),
		bearerChallengeHandler(t, server.Addr(), "repository:test/image:pull"),
		httpstub.CombineHandlers(
			httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
			httpstub.RespondWith(http.StatusOK, testManifestListBody, http.Header{
				"Content-Type": {"application/vnd.docker.distribution.manifest.list.v2+json"},
			}),
		),
	)

	container := newRegistryContainer(
		server.Addr()+"/test/image:latest",
		server.Addr()+"/test/image@sha256:"+sha256Hex(testManifestListBody),
	)

	match, remoteDigest, err := CompareDigestWithRemote(newTestLogger(t), t.Context(),
		container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
	require.NoError(t, err)
	assert.True(t, match)
	assert.Equal(t, "sha256:"+sha256Hex(testManifestListBody), remoteDigest)
}

// TestSameOrigin verifies which redirect targets count as the registry's own
// origin, so they may receive its token.
func TestSameOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{name: "same scheme and host", from: "https://registry.example.com/v2/a", to: "https://registry.example.com/b?x=1", want: true},
		{name: "host case differs", from: "https://Registry.example.com/v2/a", to: "https://registry.example.com/b", want: true},
		{name: "another host", from: "https://registry.example.com/v2/a", to: "https://cdn.example.com/b"},
		{name: "another port", from: "https://registry.example.com/v2/a", to: "https://registry.example.com:8443/b"},
		{name: "explicit default https port", from: "https://registry.example.com/v2/a", to: "https://registry.example.com:443/b", want: true},
		{name: "explicit default http port", from: "http://registry.example.com:80/v2/a", to: "http://registry.example.com/b", want: true},
		{name: "same non-default port", from: "https://registry.example.com:8443/v2/a", to: "https://REGISTRY.example.com:8443/b", want: true},
		{name: "another non-default port", from: "https://registry.example.com:8443/v2/a", to: "https://registry.example.com:9443/b"},
		{name: "ipv6 host with default port", from: "https://[::1]/v2/a", to: "https://[::1]:443/b", want: true},
		{name: "http port on https", from: "https://registry.example.com/v2/a", to: "https://registry.example.com:80/b"},
		{name: "downgrade to http", from: "https://registry.example.com/v2/a", to: "http://registry.example.com/b"},
		{name: "unparseable target", from: "https://registry.example.com/v2/a", to: "http://[::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, sameOrigin(tt.from, tt.to))
		})
	}
}

// TestFetchDigestWithGet_RedirectPastClientLimit verifies the retry Watchtower
// makes when a registry keeps redirecting a manifest request past the client's
// redirect limit. The retry goes to the redirect's whole URL, and it carries
// the registry token only when the redirect stays on the registry's origin.
func TestFetchDigestWithGet_RedirectPastClientLimit(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	tests := []struct {
		name string
		// crossOrigin sends the last redirect to another server.
		crossOrigin bool
		// wantAuthorization is the Authorization header the redirect target expects.
		wantAuthorization string
	}{
		{name: "same origin keeps the token", wantAuthorization: "Bearer mock-token"},
		{name: "another origin gets no token", crossOrigin: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := httpstub.New(t)
			other := httpstub.New(t)

			target := registry
			if tt.crossOrigin {
				target = other
			}

			// The client follows three redirects and returns the next one, so the
			// third hop's Location is the one Watchtower retries.
			redirect := func(location string) http.HandlerFunc {
				return httpstub.RespondWith(http.StatusTemporaryRedirect, "", http.Header{"Location": {location}})
			}
			registry.RouteToHandler(http.MethodGet, httpstub.Path(manifestPath), redirect("/hop1"))
			registry.RouteToHandler(http.MethodGet, httpstub.Path("/hop1"), redirect("/hop2"))
			registry.RouteToHandler(http.MethodGet, httpstub.Path("/hop2"),
				redirect("http://"+target.Addr()+"/final?sig=abc"))
			target.RouteToHandler(http.MethodGet, httpstub.Path("/final"), httpstub.CombineHandlers(
				httpstub.VerifyQuery(t, "sig=abc"),
				func(_ http.ResponseWriter, r *http.Request) {
					assert.Equal(t, tt.wantAuthorization, r.Header.Get("Authorization"))
				},
				httpstub.RespondWith(http.StatusOK, "", digestHeader(testDigestHash)),
			))
			registry.AppendHandlers(bearerChallengeHandlers(t, registry.Addr(), "repository:test/image:pull")...)

			container := newRegistryContainer(registry.Addr()+"/test/image:latest", testRepoDigest)

			got, err := fetchDigestWithGet(newTestLogger(t), t.Context(),
				container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
			require.NoError(t, err)
			assert.Equal(t, NormalizeDigest(newTestLogger(t), testDigestHash), got)
		})
	}
}

// TestFetchDigestWithGet_MirrorFailure verifies that a manifest request a
// mirror fails moves on to the next endpoint, so the registry gets its own
// challenge and never the mirror's token. This holds when the mirror's
// challenge redirects to another host too.
func TestFetchDigestWithGet_MirrorFailure(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	tests := []struct {
		name string
		// redirectChallenge sends the mirror's challenge to another host, which
		// issues the token and answers the manifest request.
		redirectChallenge bool
	}{
		{name: "mirror that challenges itself"},
		{name: "mirror whose challenge redirects to another host", redirectChallenge: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mirror := httpstub.New(t)
			mirrorAuth := mirror

			if tt.redirectChallenge {
				mirrorAuth = httpstub.New(t)
				mirror.AppendHandlers(challengeRedirectHandler(t, mirrorAuth.Addr()))
			}

			mirrorAuth.AppendHandlers(append(
				bearerChallengeHandlers(t, mirrorAuth.Addr(), "repository:test/image:pull"),
				manifestHandler(t, http.MethodGet, manifestPath, http.StatusNotFound, nil),
			)...)

			registry := httpstub.New(t)
			registry.AppendHandlers(
				anonymousRegistryHandler(t),
				httpstub.CombineHandlers(
					httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
					func(_ http.ResponseWriter, r *http.Request) {
						assert.Empty(t, r.Header.Get("Authorization"), "the mirror's token reached the registry")
					},
					httpstub.RespondWith(http.StatusOK, "", digestHeader(testDigestHash)),
				),
			)

			container := newRegistryContainer(registry.Addr()+"/test/image:latest", testRepoDigest)

			got, err := fetchDigestWithGet(newTestLogger(t), t.Context(),
				container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New(), mirror.Addr(), "")
			require.NoError(t, err)
			assert.Equal(t, NormalizeDigest(newTestLogger(t), testDigestHash), got)
			assert.Len(t, registry.ReceivedRequests(), 2,
				"the registry was not asked once through its own challenge")
		})
	}
}

// TestFetchDigestWithGet_RedirectHostFailure verifies that a manifest request
// the registry's redirect host refuses is retried once on the image registry,
// whose answer decides the result.
func TestFetchDigestWithGet_RedirectHostFailure(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	tests := []struct {
		name string
		// original answers the retry on the image registry.
		original http.HandlerFunc
		// wantErr is the error the fetch must return, nil for a digest.
		wantErr error
	}{
		{
			name:     "the registry serves the manifest",
			original: httpstub.RespondWith(http.StatusOK, "", digestHeader(testDigestHash)),
		},
		{
			name: "the registry rate limits the retry",
			original: httpstub.RespondWith(http.StatusTooManyRequests,
				"toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
				http.Header{"Retry-After": {"7200"}}),
			wantErr: ratelimit.ErrRateLimited,
		},
		{
			name:     "the registry refuses the retry",
			original: httpstub.RespondWith(http.StatusUnauthorized, "", unauthorizedChallenge("insufficient_scope")),
			wantErr:  errRepositoryDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := httpstub.New(t)
			redirectHost := httpstub.New(t)

			registry.AppendHandlers(
				challengeRedirectHandler(t, redirectHost.Addr()),
				httpstub.CombineHandlers(
					httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
					tt.original,
				),
			)
			redirectHost.AppendHandlers(append(
				bearerChallengeHandlers(t, redirectHost.Addr(), "repository:test/image:pull"),
				manifestHandler(t, http.MethodGet, manifestPath, http.StatusUnauthorized, nil),
			)...)

			container := newRegistryContainer(registry.Addr()+"/test/image:latest", testRepoDigest)

			got, err := fetchDigestWithGet(newTestLogger(t), t.Context(),
				container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, NormalizeDigest(newTestLogger(t), testDigestHash), got)
		})
	}
}

// TestCompareDigest_RedirectedLSCRImage verifies that an lscr.io image whose
// registry redirects the /v2/ challenge to another host still compares its
// digest with a HEAD request.
func TestCompareDigest_RedirectedLSCRImage(t *testing.T) {
	t.Parallel()

	server := httpstub.New(t)
	redirectServer := httpstub.New(t)

	server.AppendHandlers(
		challengeRedirectHandler(t, redirectServer.Addr()),
		httpstub.CombineHandlers(
			httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/lscr.io/test/image/manifests/latest")),
			httpstub.RespondWith(http.StatusNotFound, "", nil),
		),
		httpstub.CombineHandlers(
			httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path("/v2/lscr.io/test/image/manifests/latest")),
			httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
		),
	)
	redirectServer.AppendHandlers(
		append(bearerChallengeHandlers(t, redirectServer.Addr(), "repository:lscr.io/test/image:pull"),
			httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
		)...,
	)

	container := newRegistryContainer(server.Addr()+"/lscr.io/test/image:latest", testRepoDigest)

	got, err := CompareDigest(newTestLogger(t), context.Background(),
		container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
	require.NoError(t, err)
	assert.True(t, got)
}

// TestCompareDigestWithRemote verifies that a registry digest that differs from
// the local one is a mismatch and is returned as the remote digest.
func TestCompareDigestWithRemote(t *testing.T) {
	t.Parallel()

	const remoteHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	server := httpstub.New(t)
	server.AppendHandlers(
		anonymousRegistryHandler(t),
		manifestHandler(t, http.MethodHead, "/v2/myapp/manifests/latest", http.StatusOK, digestHeader("sha256:"+remoteHash)),
	)

	container := newRegistryContainer(server.Addr()+"/myapp:latest",
		"registry.example.com/myapp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	match, remoteDigest, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		container, "", registryConfig.Registry{}, ratelimit.New(), server.URL())
	require.NoError(t, err)
	assert.False(t, match)
	assert.Equal(t, "sha256:"+remoteHash, remoteDigest)
}

// TestCompareDigestWithRemote_SkipsInvalidEndpoint verifies that a registry
// endpoint override that is not a host is skipped for the next endpoint
// without a request to it.
func TestCompareDigestWithRemote_SkipsInvalidEndpoint(t *testing.T) {
	t.Parallel()

	const remoteHash = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	server := httpstub.New(t)
	server.AppendHandlers(
		anonymousRegistryHandler(t),
		manifestHandler(t, http.MethodHead, "/v2/myapp/manifests/latest", http.StatusOK, digestHeader(remoteHash)),
	)

	container := newRegistryContainer(server.Addr()+"/myapp:latest", "registry.example.com/myapp@"+remoteHash)

	match, _, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		container, "", registryConfig.Registry{}, ratelimit.New(), "bad host", server.URL())
	require.NoError(t, err)
	assert.True(t, match)
	assert.Len(t, server.ReceivedRequests(), 2)
}

// TestCompareDigestWithRemote_Head429DoesNotFallBackToGET verifies that a rate
// limited HEAD request returns the rate limit error instead of retrying the
// manifest with GET, which would spend more of the registry's quota.
func TestCompareDigestWithRemote_Head429DoesNotFallBackToGET(t *testing.T) {
	t.Parallel()

	server := httpstub.New(t)
	server.AppendHandlers(
		anonymousRegistryHandler(t),
		httpstub.CombineHandlers(
			httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path("/v2/linuxserver/radarr/manifests/latest")),
			httpstub.RespondWith(http.StatusTooManyRequests,
				"toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
				http.Header{"Retry-After": {"7200"}}),
		),
	)

	container := newRegistryContainer(server.Addr()+"/linuxserver/radarr:latest",
		"lscr.io/linuxserver/radarr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	match, _, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New(), server.URL())
	require.ErrorIs(t, err, ratelimit.ErrRateLimited)
	assert.False(t, match)
}

// TestCompareDigestWithRemote_429DoesNotContinueToNextEndpoint verifies that a
// 429 from one endpoint aborts the endpoint loop instead of trying the next
// mirror.
func TestCompareDigestWithRemote_429DoesNotContinueToNextEndpoint(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/linuxserver/radarr/manifests/latest"

	primary := httpstub.New(t)
	primary.AppendHandlers(
		append(bearerChallengeHandlers(t, primary.Addr(), "repository:linuxserver/radarr:pull"),
			httpstub.CombineHandlers(
				httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
				httpstub.RespondWith(http.StatusTooManyRequests,
					"toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
					http.Header{"Retry-After": {"7200"}}),
			),
		)...,
	)

	// The mirror has no handlers, so any request to it fails the test.
	mirror := httpstub.New(t)

	container := newRegistryContainer(primary.Addr()+"/linuxserver/radarr:latest",
		"lscr.io/linuxserver/radarr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	match, _, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New(), primary.URL(), mirror.URL())
	require.ErrorIs(t, err, ratelimit.ErrRateLimited)
	assert.False(t, match)
	assert.Len(t, primary.ReceivedRequests(), 3)
	assert.Empty(t, mirror.ReceivedRequests())
}

// TestCompareDigestWithRemote_UnauthorizedIsError verifies that a registry
// that answers the manifest request with a 401 that names no refused scope
// produces an error, not an empty digest that marks the image as out of date
// and causes a pull.
//
// Its token host only issues the token and gets no manifest request.
func TestCompareDigestWithRemote_UnauthorizedIsError(t *testing.T) {
	t.Parallel()

	const (
		manifestPath = "/v2/linuxserver/radarr/manifests/latest"
		scope        = "repository:linuxserver/radarr:pull"
	)

	// A manifest request to the token host has no handler and fails the test.
	tokenHost := httpstub.New(t)
	tokenHost.AppendHandlers(tokenHandler(t))

	// The registry sends clients to the token host and rejects manifest requests.
	registry := httpstub.New(t)
	registry.AppendHandlers(
		bearerChallengeHandler(t, tokenHost.Addr(), scope),
		manifestHandler(t, http.MethodHead, manifestPath, http.StatusUnauthorized, nil),
		bearerChallengeHandler(t, tokenHost.Addr(), scope),
		manifestHandler(t, http.MethodGet, manifestPath, http.StatusUnauthorized, nil),
	)

	container := newRegistryContainer(registry.Addr()+"/linuxserver/radarr:latest",
		registry.Addr()+"/linuxserver/radarr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	match, remoteDigest, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
	require.ErrorIs(t, err, errInvalidRegistryResponse)
	assert.False(t, match)
	assert.Empty(t, remoteDigest)
	assert.Len(t, tokenHost.ReceivedRequests(), 1)
}

// TestCompareDigestWithRemote_HeadRejectedFallsBackToGet verifies that when a
// registry rejects HEAD manifest requests, the check falls back to GET, which
// returns the digest.
func TestCompareDigestWithRemote_HeadRejectedFallsBackToGet(t *testing.T) {
	t.Parallel()

	const (
		manifestPath = "/v2/linuxserver/radarr/manifests/latest"
		remote       = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	registry := httpstub.New(t)
	registry.AppendHandlers(
		append(bearerChallengeHandlers(t, registry.Addr(), "repository:linuxserver/radarr:pull"),
			manifestHandler(t, http.MethodHead, manifestPath, http.StatusUnauthorized, nil),
			bearerChallengeHandler(t, registry.Addr(), "repository:linuxserver/radarr:pull"),
			httpstub.CombineHandlers(
				httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
				httpstub.RespondWith(http.StatusOK, `{"schemaVersion":2}`, http.Header{
					ContentDigestHeader: {remote},
					"Content-Type":      {"application/vnd.oci.image.index.v1+json"},
				}),
			),
		)...,
	)

	container := newRegistryContainer(registry.Addr()+"/linuxserver/radarr:latest",
		registry.Addr()+"/linuxserver/radarr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	match, remoteDigest, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
	require.NoError(t, err)
	assert.False(t, match)
	assert.Equal(t, remote, remoteDigest)
}

// TestFetchDigestForTag verifies that a release tag probe returns the tag's
// digest and fails for a HEAD response that gives none.
//
// A failed probe stays inconclusive for the caller, which retries it later. An
// empty digest would read as a tag that does not exist.
func TestFetchDigestForTag(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/v1.2.3"

	tests := []struct {
		name string
		// manifest answers the HEAD request for the tag.
		manifest http.HandlerFunc
		want     string
		wantErr  error
	}{
		{
			name:     "digest of the tag",
			manifest: httpstub.RespondWith(http.StatusOK, "", digestHeader(testDigestHash)),
			want:     NormalizeDigest(newNopLogger(), testDigestHash),
		},
		{
			name:     "a 401 without a refused scope",
			manifest: httpstub.RespondWith(http.StatusUnauthorized, "", unauthorizedChallenge("invalid_token")),
			wantErr:  errInvalidRegistryResponse,
		},
		{
			name:     "a server error",
			manifest: httpstub.RespondWith(http.StatusInternalServerError, "", nil),
			wantErr:  errInvalidRegistryResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := httpstub.New(t)
			registry.AppendHandlers(
				anonymousRegistryHandler(t),
				httpstub.CombineHandlers(
					httpstub.VerifyRequest(t, http.MethodHead, httpstub.Path(manifestPath)),
					tt.manifest,
				),
			)

			container := newRegistryContainer(registry.Addr()+"/test/image:latest", testRepoDigest)

			got, err := FetchDigestForTag(newTestLogger(t), t.Context(),
				container, "", "v1.2.3", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCompareDigestWithRemote_MirrorRefusalIsNotLocalOnly verifies that a
// mirror's refusal of a single-component name does not make the image
// local-only.
//
// The next endpoint serves the manifest, so the image matches and is probed
// again on the next check.
func TestCompareDigestWithRemote_MirrorRefusalIsNotLocalOnly(t *testing.T) {
	clearLocalOnlyImageCache()
	t.Cleanup(clearLocalOnlyImageCache)

	const manifestPath = "/v2/library/atlas-badgerdb/manifests/latest"

	mirror := httpstub.New(t)
	registry := httpstub.New(t)

	for range 2 {
		mirror.AppendHandlers(refusedProbeHandlers(t, manifestPath)...)
		registry.AppendHandlers(
			anonymousRegistryHandler(t),
			manifestHandler(t, http.MethodHead, manifestPath, http.StatusOK, digestHeader(testLocalDigest)),
		)
	}

	for range 2 {
		match, remoteDigest, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
			newLocalOnlyContainer(""), "", registryConfig.Registry{TLSSkip: true}, ratelimit.New(),
			mirror.Addr(), registry.Addr())
		require.NoError(t, err)
		assert.True(t, match)
		assert.Equal(t, testLocalDigest, remoteDigest)
	}

	assert.Len(t, mirror.ReceivedRequests(), 4, "the refused image was cached as local-only")
	assert.Len(t, registry.ReceivedRequests(), 4)
}

// TestCompareDigestWithRemote_LocalOnlyRefused is a regression test for
// locally built images (e.g. atlas-badgerdb) that have non-empty registry-less
// RepoDigests.
//
// A registry that refuses the repository leaves the image up to date with no
// error. Callers then skip the pull without logging "Digest retrieval failed,
// falling back to full pull".
func TestCompareDigestWithRemote_LocalOnlyRefused(t *testing.T) {
	clearLocalOnlyImageCache()
	t.Cleanup(clearLocalOnlyImageCache)

	server := httpstub.New(t)
	server.AppendHandlers(refusedProbeHandlers(t, "/v2/library/atlas-badgerdb/manifests/latest")...)

	match, remoteDigest, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
		newLocalOnlyContainer(""), "", registryConfig.Registry{TLSSkip: true}, ratelimit.New(), server.Addr())
	require.NoError(t, err)
	assert.True(t, match)
	assert.Empty(t, remoteDigest)
	assert.Len(t, server.ReceivedRequests(), 2)
}

// TestCompareDigestWithRemote_FailedProbeIsNotLocalOnly verifies that a probe
// that fails without a refused scope fails the check of an official Docker
// Hub image, whose repo digest names no registry host as a local build's does.
//
// The next check probes the image again. A 404 means the repository exists
// without the tag. A 401 for a token the registry does not accept, such as an
// expired one, says nothing about the repository.
func TestCompareDigestWithRemote_FailedProbeIsNotLocalOnly(t *testing.T) {
	const manifestPath = "/v2/library/nginx/manifests/latest"

	rejected := unauthorizedChallenge("invalid_token")

	tests := []struct {
		name string
		// probe returns the registry's handlers for one check.
		probe   func(t *testing.T) []http.HandlerFunc
		wantErr error
		// wantRequests is the number of requests two checks send.
		wantRequests int
	}{
		{
			name: "missing tag",
			probe: func(t *testing.T) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusNotFound, nil),
				}
			},
			wantErr:      errManifestNotFound,
			wantRequests: 4,
		},
		{
			name: "rejected token",
			probe: func(t *testing.T) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodHead, manifestPath, http.StatusUnauthorized, rejected),
					anonymousRegistryHandler(t),
					manifestHandler(t, http.MethodGet, manifestPath, http.StatusUnauthorized, rejected),
				}
			},
			wantErr:      errInvalidRegistryResponse,
			wantRequests: 8,
		},
	}

	// The subtests share the local-only cache, so they run one at a time.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearLocalOnlyImageCache()
			t.Cleanup(clearLocalOnlyImageCache)

			server := httpstub.New(t)
			server.AppendHandlers(tt.probe(t)...)
			server.AppendHandlers(tt.probe(t)...)

			container := newRegistryContainer("nginx:latest", "nginx@"+testLocalDigest)

			for range 2 {
				match, _, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
					container, "", registryConfig.Registry{TLSSkip: true}, ratelimit.New(), server.Addr())
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, match)
			}

			assert.Len(t, server.ReceivedRequests(), tt.wantRequests, "a failed probe was cached instead of repeated")
		})
	}
}

// TestIsRefusedLocalImage verifies that only a registry's refusal of a
// single-component image name without registry-qualified repo digests marks
// the image local-only.
//
// The refusal is recognized through any wrapping. A 404 and other errors never
// mark an image local-only.
func TestIsRefusedLocalImage(t *testing.T) {
	t.Parallel()

	const digestSuffix = "@" + testLocalDigest

	denied := fmt.Errorf("%w: status 401 Unauthorized", errRepositoryDenied)

	tests := []struct {
		name      string
		container types.Container
		err       error
		want      bool
	}{
		{
			name:      "true for a single-component name behind a wrapped refusal",
			container: newRegistryContainer("atlas-badgerdb", "atlas-badgerdb"+digestSuffix),
			err:       fmt.Errorf("manifest retry: %w", denied),
			want:      true,
		},
		{
			name:      "true for a single-component name with a dotted version tag",
			container: newRegistryContainer("my-app:1.0", "my-app"+digestSuffix),
			err:       denied,
			want:      true,
		},
		{
			name:      "true for a single-component name containing a dot",
			container: newRegistryContainer("my.app:latest", "my.app"+digestSuffix),
			err:       denied,
			want:      true,
		},
		{
			// Docker records an official Hub image's repo digest without a
			// registry host, as it does for a local build. Docker Hub answers a
			// missing tag of a repository it has with 404.
			name:      "false for a manifest 404 of an official Hub short name",
			container: newRegistryContainer("nginx:latest", "nginx"+digestSuffix),
			err:       fmt.Errorf("%w: %w: status 404 Not Found", errManifestNotFound, errInvalidRegistryResponse),
		},
		{
			name:      "false for an image name with a registry domain",
			container: newRegistryContainer("registry.example.com/app:latest"),
			err:       denied,
		},
		{
			// A container without image data has unknown repo digests.
			name:      "false for a container whose image could not be inspected",
			container: modelfixture.ContainerWithoutImageInfo(containerfixture.WithImageName("atlas-badgerdb")),
			err:       denied,
		},
		{
			// The image was retagged locally after a pull from a registry on
			// this machine.
			name:      "false for a single-component name whose repo digests name localhost",
			container: newRegistryContainer("app:latest", "localhost/app"+digestSuffix),
			err:       denied,
		},
		{
			// Docker counts an uppercase first component as a registry host.
			name:      "false for a single-component name whose repo digests name an uppercase host",
			container: newRegistryContainer("app:latest", "Registry/app"+digestSuffix),
			err:       denied,
		},
		{
			// The image was retagged locally after a pull from ghcr.io.
			name:      "false for a single-component name whose repo digests name a registry host",
			container: newRegistryContainer("app:latest", "ghcr.io/foo/app"+digestSuffix),
			err:       denied,
		},
		{
			// Zodiac runs the container from the image ID and records the Docker
			// Hub image it came from in a label. The probe asked for that image.
			name: "false for a namespaced image named by the Zodiac label",
			container: modelfixture.Container(
				containerfixture.WithImageName("sha256:4b1d1e6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f80"),
				containerfixture.WithRepoDigests([]string{"myorg/app" + digestSuffix}),
				containerfixture.WithLabels(map[string]string{labels.ZodiacOriginalImage: "myorg/app:latest"}),
			),
			err: denied,
		},
		{
			name:      "false for non-manifest errors",
			container: newRegistryContainer("atlas-badgerdb"),
			err:       fmt.Errorf("%w: status 500", errInvalidRegistryResponse),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isRefusedLocalImage(tt.container, tt.err))
		})
	}
}

// TestLocalOnlyImageCache ensures a confirmed local-only image skips subsequent
// registry probes for the same image name+ID.
//
// The cache keys an image by both, not by the container that runs it. Another
// container of the same image reuses the entry. A retagged image keeps its ID
// under a new name. A rebuilt image keeps its name with a new ID. Each is
// probed once and then cached beside the entries before it.
func TestLocalOnlyImageCache(t *testing.T) {
	clearLocalOnlyImageCache()
	t.Cleanup(clearLocalOnlyImageCache)

	const (
		imageID      = testLocalDigest
		manifestPath = "/v2/library/atlas-badgerdb/manifests/latest"
		// retaggedPath is the manifest of the same image under another name.
		retaggedPath = "/v2/library/atlas-badgerdb-dev/manifests/latest"
	)

	settings := registryConfig.Registry{TLSSkip: true}

	// The registry refuses three probes: the first comparison, the retagged
	// image's and the rebuilt image's. A cached comparison must not reach it.
	server := httpstub.New(t)
	server.AppendHandlers(refusedProbeHandlers(t, manifestPath)...)
	server.AppendHandlers(refusedProbeHandlers(t, retaggedPath)...)
	server.AppendHandlers(refusedProbeHandlers(t, manifestPath)...)

	// compare compares the container with the registry and expects a match.
	compare := func(container types.Container) {
		t.Helper()

		match, _, err := CompareDigestWithRemote(newTestLogger(t), context.Background(),
			container, "", settings, ratelimit.New(), server.Addr())
		require.NoError(t, err)
		assert.True(t, match)
	}

	// The image is tagged under both names, so every container of it lists
	// both tags. Only the image name a container runs tells them apart.
	bothTags := containerfixture.WithRepoTags([]string{"atlas-badgerdb:latest", "atlas-badgerdb-dev:latest"})

	built := newLocalOnlyContainer(imageID, bothTags)

	// The first comparison probes the registry and caches the image as local-only.
	compare(built)
	compare(built)
	assert.Len(t, server.ReceivedRequests(), 2, "cached local-only image must not re-probe registry")

	// Another container of the same image reuses the cached entry.
	compare(newLocalOnlyContainer(imageID, containerfixture.WithName("atlas-badgerdb-2"), bothTags))
	assert.Len(t, server.ReceivedRequests(), 2, "another container of a cached image re-probed the registry")

	// The container now runs the image under its other name, so that name is
	// probed. The container keeps its name, and the image keeps its ID, repo
	// digests and tags, so only the image name tells the two apart.
	retagged := newLocalOnlyContainer(imageID, containerfixture.WithImageName("atlas-badgerdb-dev"), bothTags)

	compare(retagged)
	compare(retagged)
	assert.Len(t, server.ReceivedRequests(), 4, "a retagged local-only image was not probed once")

	// The image was rebuilt under its first name, so it is probed again.
	rebuilt := newLocalOnlyContainer("sha256:91a18788cff68385b59a3a1799cf1d0abe5e83f06f2f152dab4076445d6ab35")

	compare(rebuilt)
	compare(rebuilt)
	assert.Len(t, server.ReceivedRequests(), 6, "a rebuilt local-only image was not probed once")

	// The first image stays cached beside the later ones.
	compare(built)
	assert.Len(t, server.ReceivedRequests(), 6, "the first local-only image was no longer cached")
}

// TestFetchDigestWithGet verifies digest retrieval from an anonymous registry's
// GET response header, JSON body, and plain text body, and the error for a
// response that carries no digest.
func TestFetchDigestWithGet(t *testing.T) {
	t.Parallel()

	const (
		manifestPath = "/v2/myapp/manifests/latest"
		localHash    = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	)

	tests := []struct {
		name string
		// manifest answers the GET manifest request.
		manifest http.HandlerFunc
		expected string
		wantErr  bool
	}{
		{
			name: "extracts digest from GET response header",
			manifest: httpstub.RespondWith(http.StatusOK,
				`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}`,
				http.Header{
					ContentDigestHeader: {localHash},
					"Content-Type":      {"application/vnd.docker.distribution.manifest.v2+json"},
				}),
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name: "extracts digest from GET response JSON body",
			manifest: httpstub.RespondWith(http.StatusOK,
				fmt.Sprintf(`{"digest":%q}`, localHash),
				http.Header{"Content-Type": {"application/vnd.docker.distribution.manifest.v2+json"}}),
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name:     "extracts digest from plain text body",
			manifest: httpstub.RespondWith(http.StatusOK, localHash, http.Header{"Content-Type": {"text/plain"}}),
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name:     "returns error when no digest header and empty body",
			manifest: httpstub.RespondWith(http.StatusOK, "", nil),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httpstub.New(t)
			server.AppendHandlers(
				anonymousRegistryHandler(t),
				httpstub.CombineHandlers(
					httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
					tt.manifest,
				),
			)

			container := newRegistryContainer("registry.example.com/myapp:latest", "registry.example.com/myapp@"+localHash)

			got, err := fetchDigestWithGet(newTestLogger(t), context.Background(),
				container, "", registryConfig.Registry{}, ratelimit.New(), server.URL())
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestFetchDigestWithGet_StubRegistry verifies digest retrieval with a GET
// request against a plain HTTP registry, and the failures that happen before
// any request is made.
func TestFetchDigestWithGet_StubRegistry(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	tests := []struct {
		name string
		// handlers returns the registry's handlers in request order.
		handlers func(t *testing.T, addr string) []http.HandlerFunc
		// image returns the image reference the container runs.
		image func(addr string) string
		want  string
		// errContains is the expected error text, empty when no error is expected.
		errContains string
	}{
		{
			name: "should fetch a digest successfully",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
					),
				)
			},
			want: NormalizeDigest(newTestLogger(t), testDigestHash),
		},
		{
			name: "should handle no authentication required",
			handlers: func(t *testing.T, _ string) []http.HandlerFunc {
				t.Helper()

				return []http.HandlerFunc{
					anonymousRegistryHandler(t),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
					),
				}
			},
			want: NormalizeDigest(newTestLogger(t), testDigestHash),
		},
		{
			name: "should return an error if GET request fails after token",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusInternalServerError, "server error", nil),
					),
				)
			},
			errContains: errInvalidRegistryResponse.Error(),
		},
		{
			name: "should handle plain text 404 responses (non-JSON body)",
			handlers: func(t *testing.T, addr string) []http.HandlerFunc {
				t.Helper()

				return append(bearerChallengeHandlers(t, addr, "repository:test/image:pull"),
					httpstub.CombineHandlers(
						httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
						httpstub.RespondWith(http.StatusOK, "404 Not Found", nil),
					),
				)
			},
			errContains: "invalid digest format in body",
		},
		{
			name:        "should return an error if manifest URL build fails",
			image:       func(string) string { return "example.com/test/image:" },
			errContains: errFailedParseImageName.Error(),
		},
		{
			name:        "should handle URL parsing failure",
			image:       func(string) string { return "http://invalid url with spaces/test/image:latest" },
			errContains: errFailedBuildManifestURL.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httpstub.New(t)
			if tt.handlers != nil {
				server.AppendHandlers(tt.handlers(t, server.Addr())...)
			}

			image := server.Addr() + "/test/image:latest"
			if tt.image != nil {
				image = tt.image(server.Addr())
			}

			container := newRegistryContainer(image, testRepoDigest)

			got, err := fetchDigestWithGet(newTestLogger(t), context.Background(),
				container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFetchDigestWithGet_LocalImages verifies that locally built images, which
// have no repo digests, skip the registry for any registry host.
func TestFetchDigestWithGet_LocalImages(t *testing.T) {
	t.Parallel()

	for _, image := range []string{
		"localhost/test/image:latest",
		"local/test/image:latest",
		"127.0.0.1/test/image:latest",
	} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()

			got, err := fetchDigestWithGet(newTestLogger(t), context.Background(),
				newRegistryContainer(image), "", registryConfig.Registry{}, ratelimit.New())
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
}

// TestFetchDigestWithGet_RedirectedChallenge verifies that when the /v2/
// challenge redirects to another host, the manifest request goes to that host.
func TestFetchDigestWithGet_RedirectedChallenge(t *testing.T) {
	t.Parallel()

	const manifestPath = "/v2/test/image/manifests/latest"

	server := httpstub.New(t)
	redirectServer := httpstub.New(t)

	server.AppendHandlers(
		challengeRedirectHandler(t, redirectServer.Addr()),
	)
	redirectServer.AppendHandlers(
		append(bearerChallengeHandlers(t, redirectServer.Addr(), "repository:test/image:pull"),
			httpstub.CombineHandlers(
				httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path(manifestPath)),
				httpstub.RespondWith(http.StatusOK, "", http.Header{ContentDigestHeader: {testDigestHash}}),
			),
		)...,
	)

	container := newRegistryContainer(server.Addr()+"/test/image:latest", testRepoDigest)

	got, err := fetchDigestWithGet(newTestLogger(t), context.Background(),
		container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
	require.NoError(t, err)
	assert.Equal(t, "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547", got)
}

// TestFetchDigestWithGet_SlowResponse verifies that a manifest request the
// registry does not answer before the context deadline fails with the deadline
// error instead of waiting for the registry.
func TestFetchDigestWithGet_SlowResponse(t *testing.T) {
	t.Parallel()

	server := httpstub.New(t)
	server.AppendHandlers(
		append(bearerChallengeHandlers(t, server.Addr(), "repository:test/image:pull"),
			httpstub.CombineHandlers(
				httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/test/image/manifests/latest")),
				func(_ http.ResponseWriter, r *http.Request) {
					// Answer only once the client gives up on the request.
					<-r.Context().Done()
				},
			),
		)...,
	)

	container := newRegistryContainer(server.Addr()+"/test/image:latest", testRepoDigest)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_, err := fetchDigestWithGet(newTestLogger(t), ctx,
		container, "token", registryConfig.Registry{TLSSkip: true}, ratelimit.New())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestBuildManifestURL verifies the manifest URL and registry host built for an
// image, with the TLS setting, host overrides, and the lscr.io redirect.
func TestBuildManifestURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tlsSkip      bool
		image        string
		hostOverride string
		expectedURL  string
		expectedHost string
		wantErr      bool
		// errIs is the cause the build error must also wrap, nil to skip the check.
		errIs error
	}{
		{
			name:         "builds HTTPS manifest URL with empty override",
			image:        "nginx:latest",
			expectedURL:  "https://index.docker.io/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
		},
		{
			name:         "builds HTTP manifest URL when TLS is skipped",
			tlsSkip:      true,
			image:        "nginx:latest",
			expectedURL:  "http://index.docker.io/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
		},
		{
			name:         "overrides host with bare host string",
			image:        "nginx:latest",
			hostOverride: "mirror.example.com",
			expectedURL:  "https://mirror.example.com/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
		},
		{
			name:         "overrides host and scheme from full URL",
			image:        "nginx:latest",
			hostOverride: "http://mirror.example.com:5000",
			expectedURL:  "http://mirror.example.com:5000/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
		},
		{
			name:         "redirects lscr.io to ghcr.io",
			image:        "lscr.io/linuxserver/radarr:latest",
			expectedURL:  "https://ghcr.io/v2/linuxserver/radarr/manifests/latest",
			expectedHost: "lscr.io",
		},
		{
			name:         "overrides host with a bare host and port",
			image:        "nginx:latest",
			hostOverride: "127.0.0.1:5000",
			expectedURL:  "https://127.0.0.1:5000/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
		},
		{
			name:    "rejects an image reference without a registry host",
			image:   "a_b.example.com/repo:tag",
			wantErr: true,
			errIs:   auth.ErrMissingRegistryHost,
		},
		{
			name:         "rejects a host override that is not a host",
			image:        "nginx:latest",
			hostOverride: "bad host",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := registryConfig.Registry{TLSSkip: tt.tlsSkip}

			gotURL, gotHost, _, err := buildManifestURL(newTestLogger(t), newRegistryContainer(tt.image), settings, tt.hostOverride)
			if tt.wantErr {
				require.ErrorIs(t, err, errFailedBuildManifestURL)

				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}

				assert.Empty(t, gotURL)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedURL, gotURL)
			assert.Equal(t, tt.expectedHost, gotHost)
		})
	}
}

// TestHandleManifestResponse verifies how manifest responses are turned into a
// digest, a retry on another host, or an error.
func TestHandleManifestResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupResponse  func() *http.Response
		method         string
		originalHost   string
		redirected     bool
		parsedURL      *url.URL
		expectedDigest string
		expectedURL    string
		expectedRetry  bool
		wantErr        bool
		// wantErrIs, when set, is the error the response must produce.
		wantErrIs error
	}{
		{
			name:   "successful HEAD 200 without digest header returns error",
			method: http.MethodHead,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			redirected:   false,
			parsedURL:    mustParseURL("https://registry.example.com/v2/manifests/latest"),
			wantErr:      true,
		},
		{
			name:   "successful GET 200 returns digest from header",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost:   "registry.example.com",
			redirected:     false,
			parsedURL:      mustParseURL("https://registry.example.com/v2/manifests/latest"),
			expectedDigest: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectedURL:    "",
			expectedRetry:  false,
			wantErr:        false,
		},
		{
			name:   "HEAD 404 returns error without retry",
			method: http.MethodHead,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Header:     http.Header{},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			redirected:   false,
			parsedURL:    mustParseURL("https://registry.example.com/v2/manifests/latest"),
			wantErr:      true,
		},
		{
			name:   "GET 404 returns error without retry",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Header:     http.Header{},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			redirected:   false,
			parsedURL:    mustParseURL("https://registry.example.com/v2/manifests/latest"),
			wantErr:      true,
		},
		{
			name:   "HEAD 401 returns an empty digest for GET fallback",
			method: http.MethodHead,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     http.Header{"Www-Authenticate": []string{"Bearer"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost:  "registry.example.com",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			expectedRetry: false,
			wantErr:       false,
		},
		{
			name:   "3xx redirect updates URL host",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusMovedPermanently,
					Status:     "301 Moved Permanently",
					Header:     http.Header{"Location": []string{"https://new-registry.example.com/v2/manifests/latest"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost:  "registry.example.com",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			expectedURL:   "https://new-registry.example.com/v2/manifests/latest",
			expectedRetry: true,
			wantErr:       false,
		},
		{
			name:   "3xx redirect keeps the Location's path and query",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusTemporaryRedirect,
					Status:     "307 Temporary Redirect",
					Header:     http.Header{"Location": []string{"/v2/mirror/manifests/latest?sig=abc"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost:  "registry.example.com",
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			expectedURL:   "https://registry.example.com/v2/mirror/manifests/latest?sig=abc",
			expectedRetry: true,
		},
		{
			name:   "3xx redirect to the request URL is not retried",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusFound,
					Status:     "302 Found",
					Header:     http.Header{"Location": []string{"/v2/manifests/latest"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			parsedURL:    mustParseURL("https://registry.example.com/v2/manifests/latest"),
			wantErr:      true,
		},
		{
			name:   "401 on redirected host retries on original host",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     http.Header{"Www-Authenticate": []string{"Bearer"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://redirect.example.com/v2/manifests/latest")},
				}
			},
			originalHost:  "registry.example.com",
			redirected:    true,
			parsedURL:     mustParseURL("https://redirect.example.com/v2/manifests/latest"),
			expectedURL:   "https://registry.example.com/v2/manifests/latest",
			expectedRetry: true,
			wantErr:       false,
		},
		{
			name:   "refused HEAD returns a refusal without GET fallback",
			method: http.MethodHead,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     unauthorizedChallenge("insufficient_scope"),
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			parsedURL:    mustParseURL("https://registry.example.com/v2/manifests/latest"),
			wantErr:      true,
			wantErrIs:    errRepositoryDenied,
		},
		{
			name:   "refused GET returns a refusal",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     unauthorizedChallenge("insufficient_scope"),
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			parsedURL:    mustParseURL("https://registry.example.com/v2/manifests/latest"),
			wantErr:      true,
			wantErrIs:    errRepositoryDenied,
		},
		{
			name:   "refused HEAD on redirected host retries on original host",
			method: http.MethodHead,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     unauthorizedChallenge("insufficient_scope"),
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://redirect.example.com/v2/manifests/latest")},
				}
			},
			originalHost:  "registry.example.com",
			redirected:    true,
			parsedURL:     mustParseURL("https://redirect.example.com/v2/manifests/latest"),
			expectedURL:   "https://registry.example.com/v2/manifests/latest",
			expectedRetry: true,
		},
		{
			// The mirror's token belongs to the mirror, and the endpoint list
			// tries the registry next.
			name:   "401 on a mirror endpoint returns error without retrying the original host",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     http.Header{"Www-Authenticate": []string{"Bearer"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://mirror.example.com/v2/manifests/latest")},
				}
			},
			originalHost: "registry.example.com",
			redirected:   false,
			parsedURL:    mustParseURL("https://mirror.example.com/v2/manifests/latest"),
			wantErr:      true,
		},
		{
			name:   "HEAD 429 returns rate-limit error without GET fallback",
			method: http.MethodHead,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Header:     http.Header{"Retry-After": []string{"2"}},
					Body: io.NopCloser(strings.NewReader(
						"toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
					)),
					Request: &http.Request{URL: mustParseURL("https://ghcr.io/v2/linuxserver/nginx/manifests/latest")},
				}
			},
			originalHost: "ghcr.io",
			redirected:   false,
			parsedURL:    mustParseURL("https://ghcr.io/v2/linuxserver/nginx/manifests/latest"),
			wantErr:      true,
		},
		{
			name:   "GET 429 returns rate-limit error",
			method: http.MethodGet,
			setupResponse: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Header:     http.Header{"Retry-After": []string{"2"}},
					Body: io.NopCloser(strings.NewReader(
						"toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
					)),
					Request: &http.Request{URL: mustParseURL("https://ghcr.io/v2/linuxserver/nginx/manifests/latest")},
				}
			},
			originalHost: "ghcr.io",
			redirected:   false,
			parsedURL:    mustParseURL("https://ghcr.io/v2/linuxserver/nginx/manifests/latest"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := tt.setupResponse()
			if resp.Body != nil {
				defer resp.Body.Close()
			}

			gotDigest, gotURL, gotRetry, err := handleManifestResponse(newTestLogger(t), resp,
				tt.method,
				tt.originalHost,
				tt.redirected,
				tt.parsedURL,
			)
			if tt.wantErr {
				require.Error(t, err)

				if resp.StatusCode == http.StatusTooManyRequests {
					require.ErrorIs(t, err, ratelimit.ErrRateLimited)
				}

				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedDigest, gotDigest)
			assert.Equal(t, tt.expectedURL, gotURL)
			assert.Equal(t, tt.expectedRetry, gotRetry)
		})
	}
}

// TestExtractHeadDigest verifies digest extraction from a HEAD response's
// Docker-Content-Digest header and the error when it is missing.
func TestExtractHeadDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupResp func() *http.Response
		expected  string
		wantErr   bool
	}{
		{
			name: "extracts digest from Docker-Content-Digest header",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}},
					Body:       http.NoBody,
				}
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "returns error when Docker-Content-Digest header is missing",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       http.NoBody,
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "includes www-authenticate header in error for 401",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     http.Header{"Www-Authenticate": []string{"Bearer realm=\"https://auth.example.com/token\""}},
					Body:       http.NoBody,
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "normalizes sha256 prefix from header",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:abc"}},
					Body:       http.NoBody,
				}
			},
			expected: "abc",
			wantErr:  false,
		},
		{
			name: "returns an empty digest for a GET fallback when Docker-Content-Digest has no hash",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:"}},
					Body:       http.NoBody,
				}
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := tt.setupResp()
			if resp.Body != nil {
				defer resp.Body.Close()
			}

			got, err := extractHeadDigest(newTestLogger(t), resp)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestExtractGetDigest verifies digest extraction from a GET response's header,
// JSON body, or plain text body, and the errors for responses without one.
func TestExtractGetDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupResp func() *http.Response
		expected  string
		wantErr   bool
		// errContains is the expected error text, empty to skip the check.
		errContains string
	}{
		{
			name: "extracts digest from response header",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}},
					Body:       http.NoBody,
				}
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "extracts digest from JSON manifest body with valid content type",
			setupResp: func() *http.Response {
				body, err := json.Marshal(map[string]string{"digest": "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"})
				if err != nil {
					panic(err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.docker.distribution.manifest.v2+json"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "returns error when JSON body has empty digest field",
			setupResp: func() *http.Response {
				body, err := json.Marshal(map[string]string{"digest": ""})
				if err != nil {
					panic(err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.oci.image.manifest.v1+json"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "rejects JSON body with unsupported content type",
			setupResp: func() *http.Response {
				body, err := json.Marshal(map[string]string{"digest": "sha256:abc"})
				if err != nil {
					panic(err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/plain"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "extracts digest from plain text body starting with sha256",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/plain"}},
					Body:       io.NopCloser(strings.NewReader("sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")),
				}
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "returns error when body is too short for plain text digest",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/plain"}},
					Body:       io.NopCloser(strings.NewReader("sha256:abc")),
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "returns error when body is empty",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("")),
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "returns error when body is whitespace only",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("   \n\t  ")),
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "returns error when body is invalid JSON without sha256 prefix",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader("not json")),
				}
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "returns error when reading body fails",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       io.NopCloser(iotest.ErrReader(errors.New("simulated read error"))),
				}
			},
			expected:    "",
			wantErr:     true,
			errContains: "failed to read response body",
		},
		{
			name: "returns error when plain text body is not a digest",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("invalid-digest-format")),
				}
			},
			wantErr:     true,
			errContains: "invalid digest format in body",
		},
		{
			name: "prefers the digest header over an unparseable body",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{testDigestHash}},
					Body:       io.NopCloser(strings.NewReader("invalid-json")),
				}
			},
			expected: NormalizeDigest(newTestLogger(t), testDigestHash),
		},
		{
			name: "extracts digest from JSON body with application/json content type",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"digest": "%s"}`, testDigestHash))),
				}
			},
			expected: NormalizeDigest(newTestLogger(t), testDigestHash),
		},
		{
			name: "extracts digest from OCI image index JSON body",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.oci.image.index.v1+json"}},
					Body: io.NopCloser(strings.NewReader(
						`{"digest": "sha256:ociindexdigest123456789012345678901234567890123456789012345678901234567890"}`,
					)),
				}
			},
			expected: "ociindexdigest123456789012345678901234567890123456789012345678901234567890",
		},
		{
			name: "returns error when JSON body has no digest field",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"other_field": "value"}`)),
				}
			},
			wantErr:     true,
			errContains: "empty digest in JSON manifest",
		},
		{
			name: "rejects JSON body without content type",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(`{"digest": "sha256:missingcontenttypedigest"}`)),
				}
			},
			wantErr:     true,
			errContains: "unsupported content type for JSON parsing",
		},
		{
			name: "falls back to the body when the digest header has no hash",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:"}},
					Body:       io.NopCloser(strings.NewReader(testDigestHash)),
				}
			},
			expected: NormalizeDigest(newTestLogger(t), testDigestHash),
		},
		{
			name: "returns error when the digest header has no hash and the body is empty",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:"}},
					Body:       http.NoBody,
				}
			},
			wantErr:     true,
			errContains: "missing digest header and empty body",
		},
		{
			name: "computes the digest of an OCI image index body without a digest field",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.oci.image.index.v1+json"}},
					Body:       io.NopCloser(strings.NewReader(testIndexBody)),
				}
			},
			expected: sha256Hex(testIndexBody),
		},
		{
			name: "computes the digest of a Docker manifest list body from its exact bytes",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.docker.distribution.manifest.list.v2+json"}},
					Body:       io.NopCloser(strings.NewReader(testManifestListBody)),
				}
			},
			expected: sha256Hex(testManifestListBody),
		},
		{
			name: "computes the digest of an OCI image manifest body without a digest field",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.oci.image.manifest.v1+json"}},
					Body:       io.NopCloser(strings.NewReader(testImageManifestBody)),
				}
			},
			expected: sha256Hex(testImageManifestBody),
		},
		{
			name: "returns error for schema 2 JSON that is not a manifest",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"schemaVersion": 2, "status": "pending"}`)),
				}
			},
			wantErr:     true,
			errContains: "empty digest in JSON manifest",
		},
		{
			name: "returns error for a schema 2 manifest without layers",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.oci.image.manifest.v1+json"}},
					Body: io.NopCloser(strings.NewReader(
						`{"schemaVersion": 2, "config": {"digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333"}}`,
					)),
				}
			},
			wantErr:     true,
			errContains: "empty digest in JSON manifest",
		},
		{
			name: "returns error for a schema 1 manifest without a digest field",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/vnd.docker.distribution.manifest.v1+prettyjws"}},
					Body:       io.NopCloser(strings.NewReader(`{"schemaVersion": 1, "name": "test/image", "tag": "latest"}`)),
				}
			},
			wantErr:     true,
			errContains: "empty digest in JSON manifest",
		},
		{
			name: "returns error when JSON body digest has no hash",
			setupResp: func() *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"digest": "sha256:"}`)),
				}
			},
			wantErr:     true,
			errContains: "empty digest in JSON manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := tt.setupResp()
			if resp.Body != nil {
				defer resp.Body.Close()
			}

			got, err := extractGetDigest(newTestLogger(t), resp)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)

				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestExtractGetDigest_ManifestSizeLimits verifies that a manifest body may be
// exactly maxManifestSize long and no longer.
func TestExtractGetDigest_ManifestSizeLimits(t *testing.T) {
	t.Parallel()

	const digestPrefix = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	tests := []struct {
		name    string
		size    int
		wantErr error
	}{
		{
			name: "accepts body of exactly maxManifestSize",
			size: maxManifestSize,
		},
		{
			name:    "rejects body of maxManifestSize plus one",
			size:    maxManifestSize + 1,
			wantErr: errManifestTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := digestPrefix + strings.Repeat("a", tc.size-len(digestPrefix))
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}

			t.Cleanup(func() { _ = resp.Body.Close() })

			got, err := extractGetDigest(newTestLogger(t), resp)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, strings.TrimPrefix(body, "sha256:"), got)
		})
	}
}

// TestMakeManifestRequest verifies the method, authorization, Accept, and
// User-Agent of manifest requests, and the error for an invalid URL.
func TestMakeManifestRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      string
		manifestURL string
		token       string
		verify      func(t *testing.T, req *http.Request, err error)
	}{
		{
			name:        "creates HEAD request without auth token",
			method:      http.MethodHead,
			manifestURL: "https://registry.example.com/v2/manifests/latest",
			token:       "",
			verify: func(t *testing.T, req *http.Request, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.Equal(t, http.MethodHead, req.Method)
				assert.Equal(t, "https://registry.example.com/v2/manifests/latest", req.URL.String())
				assert.Empty(t, req.Header.Get("Authorization"))
			},
		},
		{
			name:        "creates GET request with auth token",
			method:      http.MethodGet,
			manifestURL: "https://registry.example.com/v2/manifests/latest",
			token:       "Bearer abc123",
			verify: func(t *testing.T, req *http.Request, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "Bearer abc123", req.Header.Get("Authorization"))
			},
		},
		{
			name:        "sets Accept header for manifest request",
			method:      http.MethodHead,
			manifestURL: "https://registry.example.com/v2/manifests/latest",
			token:       "",
			verify: func(t *testing.T, req *http.Request, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, req)
				accept := req.Header.Get("Accept")
				assert.Contains(t, accept, "application/vnd.docker.distribution.manifest.v2+json")
				assert.Contains(t, accept, "application/vnd.oci.image.manifest.v1+json")
			},
		},
		{
			name:        "sets User-Agent header",
			method:      http.MethodHead,
			manifestURL: "https://registry.example.com/v2/manifests/latest",
			token:       "",
			verify: func(t *testing.T, req *http.Request, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.NotEmpty(t, req.Header.Get("User-Agent"))
			},
		},
		{
			name:        "returns error for invalid URL",
			method:      http.MethodHead,
			manifestURL: "://invalid-url",
			token:       "",
			verify: func(t *testing.T, req *http.Request, err error) {
				t.Helper()
				require.Error(t, err)
				assert.Nil(t, req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			req, err := makeManifestRequest(ctx, tt.method, tt.manifestURL, tt.token)
			tt.verify(t, req, err)
		})
	}
}

// TestRetryManifestRequest verifies the outcome of retrying a manifest request
// on another host: a digest, a GET fallback, or an error.
func TestRetryManifestRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      string
		updatedURL  string
		token       string
		setupClient func(t *testing.T) *mockAuth.MockClient
		expected    string
		wantErr     bool
		rateLimited bool
		// wantErrIs, when set, is an error the result must wrap.
		wantErrIs error
		// wantErrContains, when set, is text the error must contain.
		wantErrContains string
		// wantErrNotContains, when set, is text the error must not contain.
		wantErrNotContains string
		// currentURL is the URL the retry runs on. Empty means the original host.
		currentURL string
		// redirected reports that the retry runs on the host the registry's
		// challenge redirected to.
		redirected bool
	}{
		{
			name:       "returns digest from successful retry response",
			method:     http.MethodHead,
			updatedURL: "https://registry.example.com/v2/manifests/latest",
			token:      "Bearer token123",
			setupClient: func(t *testing.T) *mockAuth.MockClient {
				t.Helper()
				mc := mockAuth.NewMockClient(t)
				mc.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Docker-Content-Digest": []string{"sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}, nil)

				return mc
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name:       "returns error when client Do fails",
			method:     http.MethodHead,
			updatedURL: "https://registry.example.com/v2/manifests/latest",
			token:      "",
			setupClient: func(t *testing.T) *mockAuth.MockClient {
				t.Helper()
				mc := mockAuth.NewMockClient(t)
				mc.EXPECT().Do(mock.Anything).Return(nil, errors.New("connection refused"))

				return mc
			},
			expected: "",
			wantErr:  true,
		},
		{
			name:       "returns error when manifest response is invalid",
			method:     http.MethodGet,
			updatedURL: "https://registry.example.com/v2/manifests/latest",
			token:      "",
			setupClient: func(t *testing.T) *mockAuth.MockClient {
				t.Helper()
				mc := mockAuth.NewMockClient(t)
				mc.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusInternalServerError,
					Status:     "500 Internal Server Error",
					Header:     http.Header{},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}, nil)

				return mc
			},
			expected: "",
			wantErr:  true,
		},
		{
			name:       "returns rate-limit error on 429",
			method:     http.MethodHead,
			updatedURL: "https://registry.example.com/v2/manifests/latest",
			token:      "",
			setupClient: func(t *testing.T) *mockAuth.MockClient {
				t.Helper()

				header := http.Header{}
				header.Set("Retry-After", "7200")

				mc := mockAuth.NewMockClient(t)
				mc.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Header:     header,
					Body:       io.NopCloser(strings.NewReader("toomanyrequests: retry-after: 2h, allowed: 44000/minute")),
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}, nil)

				return mc
			},
			expected:    "",
			wantErr:     true,
			rateLimited: true,
		},
		{
			name:        "returns error when the GET retry on the redirect host asks for the original host",
			method:      http.MethodGet,
			updatedURL:  "https://ghcr.io/v2/manifests/latest",
			currentURL:  "https://ghcr.io/v2/manifests/latest",
			redirected:  true,
			setupClient: unauthorizedManifestClient,
			wantErr:     true,
			wantErrIs:   errUnresolvedManifestRetry,
			// The error names the original-host URL the registry asked for.
			wantErrContains: "https://registry.example.com/v2/manifests/latest",
		},
		{
			name:        "leaves the query out of the URL the registry asked for",
			method:      http.MethodGet,
			updatedURL:  "https://ghcr.io/v2/manifests/latest?sig=secret",
			currentURL:  "https://ghcr.io/v2/manifests/latest?sig=secret",
			redirected:  true,
			setupClient: unauthorizedManifestClient,
			wantErr:     true,
			wantErrIs:   errUnresolvedManifestRetry,
			// The redirect target's signed query stays on the URL Watchtower
			// would request next, but not in the error.
			wantErrContains:    "https://registry.example.com/v2/manifests/latest",
			wantErrNotContains: "sig=secret",
		},
		{
			name:       "returns an empty digest for a failed HEAD so the caller falls back to GET",
			method:     http.MethodHead,
			updatedURL: "https://registry.example.com/v2/manifests/latest",
			setupClient: func(t *testing.T) *mockAuth.MockClient {
				t.Helper()
				mc := mockAuth.NewMockClient(t)
				mc.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusInternalServerError,
					Status:     "500 Internal Server Error",
					Header:     http.Header{},
					Body:       http.NoBody,
					Request:    &http.Request{URL: mustParseURL("https://registry.example.com/v2/manifests/latest")},
				}, nil)

				return mc
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := tt.setupClient(t)

			currentURL := cmp.Or(tt.currentURL, "https://registry.example.com/v2/manifests/latest")

			got, err := retryManifestRequest(newTestLogger(t), context.Background(),
				tt.method,
				tt.updatedURL,
				tt.token,
				"registry.example.com",
				tt.redirected,
				mustParseURL(currentURL),
				"",
				client,
				ratelimit.New(),
			)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				assert.Equal(t, tt.rateLimited, ratelimit.Is(err))

				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}

				if tt.wantErrContains != "" {
					require.ErrorContains(t, err, tt.wantErrContains)
				}

				if tt.wantErrNotContains != "" {
					assert.NotContains(t, err.Error(), tt.wantErrNotContains)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
