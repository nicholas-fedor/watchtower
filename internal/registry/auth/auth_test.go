package auth

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	registryConfig "github.com/nicholas-fedor/watchtower/internal/config/registry"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	mockAuth "github.com/nicholas-fedor/watchtower/internal/registry/auth/mocks"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/testutil/httpstub"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// testBasicAuth is the base64 "user:pass" credential the stub registries expect
// in a Basic Authorization header.
const testBasicAuth = "dXNlcjpwYXNz"

// testDockerAuth is the base64 JSON credential {"username":"user","password":"pass"}
// in the form Docker supplies it, which TransformAuth turns into testBasicAuth.
const testDockerAuth = "eyJ1c2VybmFtZSI6InVzZXIiLCJwYXNzd29yZCI6InBhc3MifQ=="

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

// newImageContainer returns a mock container that runs image.
//
// Parameters:
//   - t: Test that owns the mock.
//   - image: Image reference the container runs.
//
// Returns:
//   - *mockTypes.MockContainer: Mock that answers the image name reads of the auth code.
func newImageContainer(t *testing.T, image string) *mockTypes.MockContainer {
	t.Helper()

	container := mockTypes.NewMockContainer(t)
	container.EXPECT().ImageName().Return(image).Maybe()

	return container
}

// registryImage returns the reference of an image hosted on server.
//
// Parameters:
//   - server: Stub registry that hosts the image.
//
// Returns:
//   - string: Image reference whose registry host is the server's address.
func registryImage(server *httpstub.Server) string {
	return server.Addr() + "/test/image:latest"
}

// bearerChallenge returns a bearer challenge whose realm is the HTTPS /token
// endpoint on realmAddr.
//
// Parameters:
//   - realmAddr: Host and port of the server that issues tokens.
//
// Returns:
//   - string: WWW-Authenticate header value.
func bearerChallenge(realmAddr string) string {
	return fmt.Sprintf(`Bearer realm="https://%s/token",service="test-service",scope="repository:test/image:pull"`, realmAddr)
}

// challengeHandler returns the handler of a registry's /v2/ endpoint, which
// answers with status and, when challenge is not empty, that WWW-Authenticate
// challenge.
//
// Parameters:
//   - t: Test that receives verification failures.
//   - status: Status of the challenge response.
//   - challenge: WWW-Authenticate header value, or empty for none.
//
// Returns:
//   - http.HandlerFunc: Handler for the challenge request.
func challengeHandler(t *testing.T, status int, challenge string) http.HandlerFunc {
	t.Helper()

	var header http.Header
	if challenge != "" {
		header = http.Header{challengeHeaderName: {challenge}}
	}

	return httpstub.CombineHandlers(
		httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
		httpstub.RespondWith(status, "", header),
	)
}

// tokenHandler returns the handler of a registry's /token endpoint, which
// issues the token mock-token.
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

// Test_resolveChallengeScheme verifies that challenge requests use HTTPS unless
// the settings skip TLS verification, whatever the host.
func Test_resolveChallengeScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		settings registryConfig.Registry
		want     string
	}{
		{name: "returns https for empty host", host: "", want: "https"},
		{name: "preserves https scheme by default", host: "https://example.com", want: "https"},
		{name: "returns https for non-empty host without explicit scheme", host: "example.com", want: "https"},
		{
			name:     "returns http when TLS verification is skipped",
			host:     "example.com",
			settings: registryConfig.Registry{TLSSkip: true},
			want:     "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, resolveChallengeScheme(newTestLogger(t), tt.host, tt.settings))
		})
	}
}

// TestResolveEndpoint verifies the host and scheme taken from registry
// endpoint overrides, as http and https URLs, scheme-relative URLs, and bare
// hosts with and without ports, and that any other endpoint is rejected.
func TestResolveEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		endpoint   string
		canonical  string
		scheme     string
		wantHost   string
		wantScheme string
		wantErr    bool
	}{
		{
			name:       "returns endpoint and http when endpoint is provided",
			endpoint:   "http://custom.example.com",
			canonical:  "index.docker.io",
			scheme:     "https",
			wantHost:   "custom.example.com",
			wantScheme: "http",
		},
		{
			name:       "returns canonical host and https when endpoint is empty",
			endpoint:   "",
			canonical:  "index.docker.io",
			scheme:     "https",
			wantHost:   "index.docker.io",
			wantScheme: "https",
		},
		{
			name:       "returns empty host and https when both are empty",
			endpoint:   "",
			canonical:  "",
			scheme:     "https",
			wantHost:   "",
			wantScheme: "https",
		},
		{
			name:       "bare endpoint preserves provided scheme",
			endpoint:   "custom.example.com",
			canonical:  "index.docker.io",
			scheme:     "http",
			wantHost:   "custom.example.com",
			wantScheme: "http",
		},
		{
			name:       "bare endpoint falls back to https when scheme is https",
			endpoint:   "custom.example.com",
			canonical:  "index.docker.io",
			scheme:     "https",
			wantHost:   "custom.example.com",
			wantScheme: "https",
		},
		{
			name:       "URL endpoint keeps its port",
			endpoint:   "https://mirror.example.com:5000",
			scheme:     "http",
			wantHost:   "mirror.example.com:5000",
			wantScheme: "https",
		},
		{
			name:       "scheme-relative endpoint keeps the provided scheme",
			endpoint:   "//mirror.example.com",
			scheme:     "http",
			wantHost:   "mirror.example.com",
			wantScheme: "http",
		},
		{
			name:       "bare host with port",
			endpoint:   "mirror.example.com:5000",
			scheme:     "https",
			wantHost:   "mirror.example.com:5000",
			wantScheme: "https",
		},
		{
			name:       "bare IPv4 address with port",
			endpoint:   "127.0.0.1:5000",
			scheme:     "http",
			wantHost:   "127.0.0.1:5000",
			wantScheme: "http",
		},
		{
			name:       "bare IPv6 address with port",
			endpoint:   "[::1]:5000",
			scheme:     "http",
			wantHost:   "[::1]:5000",
			wantScheme: "http",
		},
		{name: "rejects a host with spaces", endpoint: "bad host", scheme: "https", wantErr: true},
		{name: "rejects a URL whose host has spaces", endpoint: "https://bad host", scheme: "https", wantErr: true},
		{name: "rejects a bare host with a path", endpoint: "mirror.example.com/v2", scheme: "https", wantErr: true},
		{name: "rejects a bare host with a query", endpoint: "mirror.example.com?x=1", scheme: "https", wantErr: true},
		{name: "rejects a bare host with user info", endpoint: "user@mirror.example.com", scheme: "https", wantErr: true},
		{name: "rejects a bare host with a non-numeric port", endpoint: "mirror.example.com:abc", scheme: "https", wantErr: true},
		{name: "rejects a URL with an unsupported scheme", endpoint: "ftp://mirror.example.com", scheme: "https", wantErr: true},
		{name: "rejects a port without a host", endpoint: ":5000", scheme: "https", wantErr: true},
		{name: "rejects a bare IPv6 address without brackets", endpoint: "::", scheme: "http", wantErr: true},
		{name: "rejects a host made of colons and digits", endpoint: ":0:0", scheme: "https", wantErr: true},
		{name: "rejects a URL whose host has no name", endpoint: "http://:5000", scheme: "https", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotHost, gotScheme, err := ResolveEndpoint(tt.endpoint, tt.canonical, tt.scheme)
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidEndpoint)
				assert.Empty(t, gotHost)
				assert.Empty(t, gotScheme)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, gotHost)
			assert.Equal(t, tt.wantScheme, gotScheme)
		})
	}
}

// TestGetChallengeURL verifies the /v2/ challenge URL built for an image: Docker
// Hub references use index.docker.io, lscr.io references use ghcr.io, other
// registries pass through, and an endpoint override supplies the host and, when
// it is a URL, the scheme.
func TestGetChallengeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		imageRef   string
		endpoint   string
		wantHost   string
		wantScheme string
		wantErr    bool
		// errIs is the sentinel the error must wrap, nil to skip the check.
		errIs error
	}{
		{
			name:       "docker.io maps to index.docker.io",
			imageRef:   "docker.io/library/nginx:latest",
			wantHost:   "index.docker.io",
			wantScheme: "https",
		},
		{
			name:       "reference without a registry uses Docker Hub",
			imageRef:   "nickfedor/watchtower:latest",
			wantHost:   "index.docker.io",
			wantScheme: "https",
		},
		{
			name:       "bare endpoint overrides host",
			imageRef:   "docker.io/library/nginx:latest",
			endpoint:   "mirror.example.com",
			wantHost:   "mirror.example.com",
			wantScheme: "https",
		},
		{
			name:       "https endpoint overrides host and scheme",
			imageRef:   "docker.io/library/nginx:latest",
			endpoint:   "https://mirror.example.com",
			wantHost:   "mirror.example.com",
			wantScheme: "https",
		},
		{
			name:       "http endpoint keeps the http scheme",
			imageRef:   "docker.io/library/nginx:latest",
			endpoint:   "http://mirror.example.com",
			wantHost:   "mirror.example.com",
			wantScheme: "http",
		},
		{
			name:       "endpoint keeps its port",
			imageRef:   "docker.io/library/nginx:latest",
			endpoint:   "https://mirror.example.com:5000",
			wantHost:   "mirror.example.com:5000",
			wantScheme: "https",
		},
		{
			name:       "lscr.io maps to ghcr.io",
			imageRef:   "lscr.io/linuxserver/nginx:latest",
			wantHost:   "ghcr.io",
			wantScheme: "https",
		},
		{
			name:       "endpoint overrides the lscr.io mapping",
			imageRef:   "lscr.io/library/nginx:latest",
			endpoint:   "https://mirror.example.com",
			wantHost:   "mirror.example.com",
			wantScheme: "https",
		},
		{
			name:       "non-docker host passes through",
			imageRef:   "ghcr.io/example/app:latest",
			wantHost:   "ghcr.io",
			wantScheme: "https",
		},
		{
			name:       "localhost passes through",
			imageRef:   "localhost:5000/myimage:latest",
			wantHost:   "localhost:5000",
			wantScheme: "https",
		},
		{
			name:     "invalid image ref returns error",
			imageRef: ":::invalid:::",
			wantErr:  true,
		},
		{
			name:     "invalid endpoint returns error",
			imageRef: "docker.io/library/nginx:latest",
			endpoint: "invalid url with spaces",
			wantErr:  true,
			errIs:    errInvalidEndpoint,
		},
		{
			name:     "reference without a registry host returns error",
			imageRef: "a_b.example.com/repo:tag",
			wantErr:  true,
			errIs:    ErrMissingRegistryHost,
		},
		{
			name:       "endpoint supplies the host for a reference without one",
			imageRef:   "a_b.example.com/repo:tag",
			endpoint:   "mirror.example.com",
			wantHost:   "mirror.example.com",
			wantScheme: "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref, err := reference.ParseNormalizedNamed(tt.imageRef)
			if err != nil {
				require.True(t, tt.wantErr, "unexpected parse error: %v", err)

				return
			}

			got, err := getChallengeURL(newTestLogger(t), ref, registryConfig.Registry{}, tt.endpoint)
			if tt.wantErr {
				require.ErrorIs(t, err, tt.errIs)
				assert.Empty(t, got.Host)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, url.URL{Scheme: tt.wantScheme, Host: tt.wantHost, Path: "/v2/"}, got)
		})
	}
}

// TestGetChallengeRequest verifies that a challenge request is a GET for the
// challenge URL carrying the caller's context, a wildcard Accept header, and the
// Watchtower User-Agent, and that a URL that cannot be requested is an error.
func TestGetChallengeRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     url.URL
		wantURL string
		wantErr bool
	}{
		{
			name:    "creates GET request with headers and context",
			url:     url.URL{Scheme: "https", Host: "example.com", Path: "/v2/"},
			wantURL: "https://example.com/v2/",
		},
		{
			name:    "empty URL still creates request",
			url:     url.URL{},
			wantURL: "",
		},
		{
			name:    "invalid scheme returns error",
			url:     url.URL{Scheme: "://", Host: "example.com"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			got, err := getChallengeRequest(newTestLogger(t), ctx, tt.url)
			if tt.wantErr {
				require.ErrorIs(t, err, errFailedCreateChallengeRequest)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, http.MethodGet, got.Method)
			assert.Equal(t, tt.wantURL, got.URL.String())
			assert.Equal(t, "*/*", got.Header.Get("Accept"))
			assert.Equal(t, meta.UserAgent, got.Header.Get("User-Agent"))
			assert.Equal(t, ctx, got.Context())
		})
	}
}

// Test_handleSuccessfulChallenge verifies that a challenge needing no
// authentication yields no token and keeps the redirect details.
func Test_handleSuccessfulChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		redirected   bool
		redirectHost string
		want         TokenResult
	}{
		{
			name: "non-redirected response",
			want: TokenResult{},
		},
		{
			name:         "redirected response",
			redirected:   true,
			redirectHost: "new-host.example.com",
			want:         TokenResult{Redirected: true, RedirectHost: "new-host.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, handleSuccessfulChallenge(tt.redirected, tt.redirectHost))
		})
	}
}

// Test_handleBasicAuthChallenge verifies that a Basic challenge returns the
// credentials as a Basic token. It is rejected without credentials, after a
// redirect to another host, or after a redirect from HTTPS to HTTP.
func Test_handleBasicAuthChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registryAuth string
		redirected   bool
		redirectHost string
		originalHost string
		downgraded   bool
		want         TokenResult
		wantErr      error
	}{
		{
			name:         "valid registry auth returns token result",
			registryAuth: "dGVzdA==",
			originalHost: "registry.example.com",
			want:         TokenResult{Token: "Basic dGVzdA=="},
		},
		{
			name:         "empty registry auth returns error",
			originalHost: "registry.example.com",
			wantErr:      errNoCredentials,
		},
		{
			name:         "cross-origin redirect returns error",
			registryAuth: "dGVzdA==",
			redirected:   true,
			redirectHost: "evil.example.com",
			originalHost: "registry.example.com",
			wantErr:      errCrossOriginRedirect,
		},
		{
			name:         "same-host https redirect returns token result",
			registryAuth: "dGVzdA==",
			redirected:   true,
			redirectHost: "registry.example.com",
			originalHost: "registry.example.com",
			want:         TokenResult{Token: "Basic dGVzdA==", Redirected: true, RedirectHost: "registry.example.com"},
		},
		{
			name:         "same-host https to http downgrade returns error",
			registryAuth: "dGVzdA==",
			redirected:   true,
			redirectHost: "registry.example.com",
			originalHost: "registry.example.com",
			downgraded:   true,
			wantErr:      errInsecureChallengeRedirect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := handleBasicAuthChallenge(newTestLogger(t), tt.registryAuth, tt.redirected, tt.redirectHost, tt.originalHost, tt.downgraded)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test_handleUnsupportedChallenge verifies that any challenge that reaches the
// unsupported handler is an unsupported challenge error.
func Test_handleUnsupportedChallenge(t *testing.T) {
	t.Parallel()

	for _, challenge := range []string{`basic realm="test"`, ""} {
		t.Run(challenge, func(t *testing.T) {
			t.Parallel()

			_, err := handleUnsupportedChallenge(newTestLogger(t), challenge)
			require.ErrorIs(t, err, errUnsupportedChallenge)
		})
	}
}

// Test_processChallengeResponse verifies that a challenge response is routed by
// its status and WWW-Authenticate header: 200 needs no token, Basic and Bearer
// challenges go to their handlers, a missing header is an error that tells 401
// from other statuses, and any other challenge is unsupported.
//
// The test resets the shared token cache, so it does not run in parallel.
func Test_processChallengeResponse(t *testing.T) {
	resetTokenCache(t)

	tests := []struct {
		name         string
		response     challengeResponse
		registryAuth string
		originalHost string
		downgraded   bool
		setupMock    func(*mockAuth.MockClient)
		want         TokenResult
		wantErr      error
	}{
		{
			name: "401 with bearer challenge routes to bearer auth",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Bearer realm="https://challenge.test.com/token",service="challenge.test.com"`,
			},
			originalHost: "challenge.test.com",
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"test-token"}`)),
				}, nil).Once()
			},
			want: TokenResult{Token: "Bearer test-token", ChallengeHost: "challenge.test.com"},
		},
		{
			name:     "200 with empty auth header returns success",
			response: challengeResponse{statusCode: http.StatusOK},
			want:     TokenResult{},
		},
		{
			name: "401 with basic auth challenge",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Basic realm="test"`,
			},
			registryAuth: "dGVzdA==",
			originalHost: "registry.example.com",
			want:         TokenResult{Token: "Basic dGVzdA=="},
		},
		{
			name: "cross-origin redirect with basic auth challenge returns error",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Basic realm="test"`,
				redirected:    true,
				redirectHost:  "evil.example.com",
			},
			registryAuth: "dGVzdA==",
			originalHost: "registry.example.com",
			wantErr:      errCrossOriginRedirect,
		},
		{
			name: "same-host https to http downgrade with basic auth challenge returns error",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Basic realm="test"`,
				redirected:    true,
				redirectHost:  "registry.example.com",
			},
			registryAuth: "dGVzdA==",
			originalHost: "registry.example.com",
			downgraded:   true,
			wantErr:      errInsecureChallengeRedirect,
		},
		{
			// The mock expects no request, so a token request fails the test.
			name: "https to http downgrade with bearer challenge and credentials returns error",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Bearer realm="https://attacker.example.com/token",service="registry.example.com"`,
				redirected:    true,
				redirectHost:  "registry.example.com",
			},
			registryAuth: "dXNlcjpwYXNz",
			originalHost: "registry.example.com",
			downgraded:   true,
			wantErr:      errInsecureChallengeRedirect,
		},
		{
			name:     "401 without WWW-Authenticate returns no credentials error",
			response: challengeResponse{statusCode: http.StatusUnauthorized},
			wantErr:  errNoCredentials,
		},
		{
			name:     "non-200 without WWW-Authenticate returns unexpected status error",
			response: challengeResponse{statusCode: http.StatusInternalServerError},
			wantErr:  errUnexpectedStatus,
		},
		{
			name: "digest challenge is unsupported",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Digest realm="test"`,
			},
			registryAuth: "dGVzdA==",
			wantErr:      errUnsupportedChallenge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockAuth.NewMockClient(t)
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			got, err := processChallengeResponse(newTestLogger(t), t.Context(),
				newImageContainer(t, "test/image"),
				tt.registryAuth,
				client,
				registryConfig.Registry{},
				tt.response.redirected,
				tt.response.redirectHost,
				tt.originalHost,
				tt.downgraded,
				tt.response,
			)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test_handleBearerAuth verifies that a Bearer challenge yields the fetched
// token and the challenge host, and that a failed request or an empty token is
// an error. After a redirect from HTTPS to HTTP, a challenge with credentials
// is rejected before any token request, and one without credentials is not.
//
// The test resets the shared token cache, so it does not run in parallel.
func Test_handleBearerAuth(t *testing.T) {
	resetTokenCache(t)

	tests := []struct {
		name          string
		wwwAuthHeader string
		registryAuth  string
		redirected    bool
		redirectHost  string
		downgraded    bool
		// setupMock sets the client's expectations. Nil expects no request.
		setupMock func(*mockAuth.MockClient)
		want      TokenResult
		wantErr   error
	}{
		{
			name:          "successful bearer auth",
			wwwAuthHeader: `Bearer realm="https://success.test.com/token",service="success.test.com"`,
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"test-token"}`)),
				}, nil).Once()
			},
			want: TokenResult{Token: "Bearer test-token", ChallengeHost: "success.test.com"},
		},
		{
			name:          "client Do error returns wrapped error",
			wwwAuthHeader: `Bearer realm="https://error.test.com/token",service="error.test.com"`,
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(nil, assert.AnError).Once()
			},
			wantErr: errFailedExecuteBearerRequest,
		},
		{
			name:          "empty token returns error",
			wwwAuthHeader: `Bearer realm="https://empty.test.com/token",service="empty.test.com"`,
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":""}`)),
				}, nil).Once()
			},
			wantErr: errFailedDecodeResponse,
		},
		{
			name:          "downgrade with credentials returns error without a token request",
			wwwAuthHeader: `Bearer realm="https://attacker.example.com/token",service="registry.example.com"`,
			registryAuth:  "dXNlcjpwYXNz",
			redirected:    true,
			redirectHost:  "registry.example.com",
			downgraded:    true,
			wantErr:       errInsecureChallengeRedirect,
		},
		{
			name:          "downgrade without credentials fetches an anonymous token",
			wwwAuthHeader: `Bearer realm="https://anonymous.test.com/token",service="anonymous.test.com"`,
			redirected:    true,
			redirectHost:  "registry.example.com",
			downgraded:    true,
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"anonymous-token"}`)),
				}, nil).Once()
			},
			want: TokenResult{
				Token:         "Bearer anonymous-token",
				ChallengeHost: "anonymous.test.com",
				Redirected:    true,
				RedirectHost:  "registry.example.com",
			},
		},
		{
			name:          "cross-origin https redirect with credentials fetches a token",
			wwwAuthHeader: `Bearer realm="https://ghcr.io/token",service="ghcr.io"`,
			registryAuth:  "dXNlcjpwYXNz",
			redirected:    true,
			redirectHost:  "ghcr.io",
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"ghcr-token"}`)),
				}, nil).Once()
			},
			want: TokenResult{
				Token:         "Bearer ghcr-token",
				ChallengeHost: "ghcr.io",
				Redirected:    true,
				RedirectHost:  "ghcr.io",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockAuth.NewMockClient(t)
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			got, err := handleBearerAuth(newTestLogger(t), t.Context(), tt.wwwAuthHeader,
				newImageContainer(t, "test/image"), tt.registryAuth, client, registryConfig.Registry{},
				tt.redirected, tt.redirectHost, "lscr.io", tt.downgraded)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test_challengeDowngraded verifies that a challenge counts as downgraded when
// it starts on HTTPS and any request in its redirect chain uses HTTP, including
// a hop in the middle of a chain that ends on HTTPS.
func Test_challengeDowngraded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		challenge string
		// chain lists the request URLs in the order the client sent them.
		chain []string
		want  bool
	}{
		{name: "no redirect", challenge: "https://registry.example.com/v2/", chain: []string{"https://registry.example.com/v2/"}},
		{
			name:      "https redirect to another host",
			challenge: "https://lscr.io/v2/",
			chain:     []string{"https://lscr.io/v2/", "https://ghcr.io/v2/"},
		},
		{
			name:      "redirect to http",
			challenge: "https://registry.example.com/v2/",
			chain:     []string{"https://registry.example.com/v2/", "http://registry.example.com/v2/"},
			want:      true,
		},
		{
			name:      "http hop before an https challenge",
			challenge: "https://registry.example.com/v2/",
			chain:     []string{"https://registry.example.com/v2/", "http://hop.example.com/v2/", "https://other.example.com/v2/"},
			want:      true,
		},
		{
			name:      "challenge already on http",
			challenge: "http://registry.example.com/v2/",
			chain:     []string{"http://registry.example.com/v2/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, challengeDowngraded(*mustParseURL(tt.challenge), redirectChain(tt.chain)))
		})
	}
}

// redirectChain builds the requests of a redirect chain as the HTTP client
// links them, each request after the first pointing to the response that
// redirected it.
//
// Parameters:
//   - urls: Request URLs in the order the client sent them.
//
// Returns:
//   - *http.Request: The last request in the chain.
func redirectChain(urls []string) *http.Request {
	var request *http.Request

	for _, raw := range urls {
		next := &http.Request{URL: mustParseURL(raw)}
		if request != nil {
			next.Response = &http.Response{Request: request}
		}

		request = next
	}

	return request
}

// TestGetToken_bearerChallengeDowngrade runs the challenge exchange against an
// HTTPS registry that redirects its challenge to plain HTTP. With credentials,
// GetToken fails before any token request. Without them, it fetches a token.
//
// The test resets the shared token cache, so it does not run in parallel.
func TestGetToken_bearerChallengeDowngrade(t *testing.T) {
	tests := []struct {
		name         string
		registryAuth string
		wantErr      error
	}{
		{name: "with credentials", registryAuth: "dXNlcjpwYXNz", wantErr: errInsecureChallengeRedirect},
		{name: "without credentials"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTokenCache(t)

			// The registry serves HTTPS and sends the challenge request to a
			// plain HTTP server, whose challenge names the registry's realm.
			registry := httpstub.NewTLS(t)
			plain := httpstub.New(t)

			registry.AppendHandlers(httpstub.RespondWith(http.StatusTemporaryRedirect, "",
				http.Header{"Location": {plain.URL() + "/v2/"}}))
			plain.AppendHandlers(challengeHandler(t, http.StatusUnauthorized, bearerChallenge(registry.Addr())))

			if tt.wantErr == nil {
				registry.AppendHandlers(tokenHandler(t))
			}

			got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, "test/image"),
				tt.registryAuth, &registryClient{client: registry.Client()}, registryConfig.Registry{},
				registry.URL())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "Bearer mock-token", got.Token)
		})
	}
}

// TestGetToken verifies GetToken's handling of the challenge exchange with a
// mock client: an unparseable image name, a registry that needs no token, a
// Bearer token fetch, a failed challenge request, and a rate-limited challenge.
//
// The test resets the shared token cache, so it does not run in parallel.
func TestGetToken(t *testing.T) {
	resetTokenCache(t)

	tests := []struct {
		name      string
		image     string
		setupMock func(*mockAuth.MockClient)
		want      TokenResult
		wantErr   error
	}{
		{
			name:    "empty image name fails to parse",
			image:   "",
			wantErr: errFailedParseImageName,
		},
		{
			name:  "localhost registry returns success with empty token",
			image: "localhost:5000/test/image:latest",
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
					Request:    &http.Request{URL: mustParseURL("https://localhost:5000/v2/")},
				}, nil).Once()
			},
			want: TokenResult{},
		},
		{
			name:  "successful token fetch",
			image: "test/image:latest",
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       http.NoBody,
					Header:     http.Header{"Www-Authenticate": []string{`Bearer realm="https://index.docker.io/token",service="index.docker.io"`}},
					Request:    &http.Request{URL: mustParseURL("https://index.docker.io/v2/")},
				}, nil).Once()

				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"test-token","expires_in":3600}`)),
					Header:     make(http.Header),
					Request:    &http.Request{URL: mustParseURL("https://index.docker.io/token?service=index.docker.io&scope=repository%3Atest%2Fimage%3Apull")},
				}, nil).Once()
			},
			want: TokenResult{Token: "Bearer test-token", ChallengeHost: "index.docker.io"},
		},
		{
			name:  "client Do error returns wrapped error",
			image: "test/image:latest",
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
					Request:    &http.Request{URL: mustParseURL("https://index.docker.io/v2/")},
				}, assert.AnError).Once()
			},
			wantErr: errFailedExecuteChallengeRequest,
		},
		{
			name:  "challenge 429 returns rate-limit error",
			image: "test/image:latest",
			setupMock: func(client *mockAuth.MockClient) {
				client.EXPECT().Do(mock.Anything).Return(&http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body: io.NopCloser(strings.NewReader(
						"toomanyrequests: retry-after: 331.163µs, allowed: 44000/minute",
					)),
					Header:  http.Header{"Retry-After": []string{"7200"}},
					Request: &http.Request{URL: mustParseURL("https://index.docker.io/v2/")},
				}, nil).Once()
			},
			wantErr: ratelimit.ErrRateLimited,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockAuth.NewMockClient(t)
			if tt.setupMock != nil {
				tt.setupMock(client)
			}

			got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, tt.image), "", client, registryConfig.Registry{}, "")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got.Token)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGetToken_anonymousGHCRSkipsChallengeOnCachedToken verifies that an
// anonymous GHCR request with a shared token cached makes no request.
//
// The test seeds the shared token cache, so it does not run in parallel.
func TestGetToken_anonymousGHCRSkipsChallengeOnCachedToken(t *testing.T) {
	seedAnonymousGHCRToken(t)

	got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, "ghcr.io/linuxserver/sonarr:latest"), "", mockAuth.NewMockClient(t), registryConfig.Registry{}, "")
	require.NoError(t, err)
	assert.Equal(t, TokenResult{Token: testAnonymousGHCRToken, ChallengeHost: "ghcr.io"}, got)
}

// TestGetToken_anonymousLSCRSkipsChallengeOnCachedToken verifies that an
// anonymous lscr.io request uses the shared GHCR token, since lscr.io images
// are hosted on GHCR.
//
// The test seeds the shared token cache, so it does not run in parallel.
func TestGetToken_anonymousLSCRSkipsChallengeOnCachedToken(t *testing.T) {
	seedAnonymousGHCRToken(t)

	got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, "lscr.io/linuxserver/sonarr:latest"), "", mockAuth.NewMockClient(t), registryConfig.Registry{}, "")
	require.NoError(t, err)
	assert.Equal(t, TokenResult{Token: testAnonymousGHCRToken, ChallengeHost: "ghcr.io"}, got)
}

// TestGetToken_anonymousGHCRChallengesWhenCacheEmpty verifies that an anonymous
// GHCR request without a cached token goes through the challenge and fetches
// a token.
//
// The test resets the shared token cache, so it does not run in parallel.
func TestGetToken_anonymousGHCRChallengesWhenCacheEmpty(t *testing.T) {
	resetTokenCache(t)

	client := mockAuth.NewMockClient(t)
	client.EXPECT().Do(mock.Anything).Return(&http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       http.NoBody,
		Header:     http.Header{"Www-Authenticate": []string{`Bearer realm="https://ghcr.io/token",service="ghcr.io"`}},
		Request:    &http.Request{URL: mustParseURL("https://ghcr.io/v2/")},
	}, nil).Once()
	client.EXPECT().Do(mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"token":"fresh-ghcr-token","expires_in":3600}`)),
		Header:     make(http.Header),
		Request:    &http.Request{URL: mustParseURL("https://ghcr.io/token")},
	}, nil).Once()

	got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, "ghcr.io/linuxserver/sonarr:latest"), "", client, registryConfig.Registry{}, "")
	require.NoError(t, err)
	assert.Equal(t, TokenResult{Token: "Bearer fresh-ghcr-token", ChallengeHost: "ghcr.io"}, got)
}

// TestGetToken_anonymousGHCRDoesNotSkipMirrorOrCredentials verifies that a
// cached anonymous GHCR token is not used for requests with credentials or to
// a mirror, which still go through the challenge.
//
// The test seeds the shared token cache, so it does not run in parallel.
func TestGetToken_anonymousGHCRDoesNotSkipMirrorOrCredentials(t *testing.T) {
	seedAnonymousGHCRToken(t)

	tests := []struct {
		name         string
		registryAuth string
		endpoint     string
		challengeURL string
	}{
		{
			name:         "credentials still challenge",
			registryAuth: testBasicAuth,
			challengeURL: "https://ghcr.io/v2/",
		},
		{
			name:         "mirror endpoint still challenge",
			endpoint:     "https://mirror.example.com",
			challengeURL: "https://mirror.example.com/v2/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mockAuth.NewMockClient(t)
			client.EXPECT().Do(mock.Anything).Return(&http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
				Header:     make(http.Header),
				Request:    &http.Request{URL: mustParseURL(tt.challengeURL)},
			}, nil).Once()

			got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, "ghcr.io/linuxserver/sonarr:latest"), tt.registryAuth, client, registryConfig.Registry{}, tt.endpoint)
			require.NoError(t, err)
			assert.Equal(t, TokenResult{}, got)
		})
	}
}

// TestGetToken_registryChallenges verifies GetToken against stub registries
// that answer the challenge with Basic, unsupported, or no authentication, over
// HTTP with TLS verification skipped and over HTTPS.
func TestGetToken_registryChallenges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// plainHTTP serves the registry over HTTP and skips TLS verification.
		plainHTTP bool
		status    int
		challenge string
		// dockerAuth is the credential in the form Docker supplies it.
		dockerAuth string
		want       TokenResult
		wantErr    error
	}{
		{
			name:       "basic challenge returns the credentials",
			status:     http.StatusUnauthorized,
			challenge:  `Basic realm="test"`,
			dockerAuth: testDockerAuth,
			want:       TokenResult{Token: "Basic " + testBasicAuth},
		},
		{
			name:      "basic challenge without credentials fails",
			status:    http.StatusUnauthorized,
			challenge: `Basic realm="test"`,
			wantErr:   errNoCredentials,
		},
		{
			name:       "unsupported challenge fails",
			status:     http.StatusUnauthorized,
			challenge:  `Digest realm="test"`,
			dockerAuth: testDockerAuth,
			wantErr:    errUnsupportedChallenge,
		},
		{
			name:      "401 without a challenge fails",
			plainHTTP: true,
			status:    http.StatusUnauthorized,
			wantErr:   errNoCredentials,
		},
		{
			name:      "plain HTTP registry with TLS skip needs no token",
			plainHTTP: true,
			status:    http.StatusOK,
			want:      TokenResult{},
		},
		{
			name:   "HTTPS registry needs no token",
			status: http.StatusOK,
			want:   TokenResult{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var server *httpstub.Server
			if tt.plainHTTP {
				server = httpstub.New(t)
			} else {
				server = httpstub.NewTLS(t)
			}

			server.AppendHandlers(challengeHandler(t, tt.status, tt.challenge))

			got, err := GetToken(newTestLogger(t), t.Context(),
				newImageContainer(t, registryImage(server)),
				TransformAuth(newTestLogger(t), tt.dockerAuth),
				server.Client(),
				registryConfig.Registry{TLSSkip: tt.plainHTTP},
				"",
			)

			assert.Len(t, server.ReceivedRequests(), 1)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got.Token)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGetToken_connectionFailures verifies that GetToken reports a challenge
// request that cannot be made: a registry that is not listening, an HTTPS
// request to a plain HTTP registry, and a registry whose TLS version is below
// the client's minimum.
func TestGetToken_connectionFailures(t *testing.T) {
	t.Parallel()

	t.Run("closed registry", func(t *testing.T) {
		t.Parallel()

		server := httpstub.New(t)
		image := registryImage(server)
		server.Close()

		settings := registryConfig.Registry{TLSSkip: true}

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, image), testBasicAuth, NewAuthClient(nil, settings), settings, "")
		require.ErrorIs(t, err, errFailedExecuteChallengeRequest)
		assert.Empty(t, got.Token)
	})

	t.Run("HTTPS request to a plain HTTP registry", func(t *testing.T) {
		t.Parallel()

		// The server has no handlers, since the TLS handshake never reaches one.
		server := httpstub.New(t)

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(server)), "", NewAuthClient(nil, registryConfig.Registry{}), registryConfig.Registry{}, "")
		require.ErrorIs(t, err, errFailedExecuteChallengeRequest)
		require.ErrorContains(t, err, "http: server gave HTTP response to HTTPS client")
		assert.Empty(t, got.Token)
		assert.Empty(t, server.ReceivedRequests())
	})

	t.Run("TLS version below the client minimum", func(t *testing.T) {
		t.Parallel()

		// The server has no handlers, since the TLS handshake never reaches one.
		server := httpstub.NewUnstarted(t)
		server.TestServer().TLS = &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
		server.TestServer().StartTLS()

		transport, ok := server.Client().Transport.(*http.Transport)
		require.True(t, ok, "unexpected transport type %T", server.Client().Transport)

		transport = transport.Clone()
		transport.TLSClientConfig.MinVersion = tls.VersionTLS13

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(server)), "", &http.Client{Transport: transport}, registryConfig.Registry{}, "")
		require.ErrorIs(t, err, errFailedExecuteChallengeRequest)
		assert.Empty(t, got.Token)
	})
}

// TestGetToken_bearerChallenges verifies that GetToken fetches a bearer token
// from the realm a stub registry names, whether that is a separate token
// server or the registry itself, that it derives the token service from the
// realm when the challenge has none, and that it reports a redirected
// challenge.
//
// A stub registry can reuse the port of an earlier one, whose token would then
// be cached for it, so each subtest resets the shared token cache and the test
// does not run in parallel.
func TestGetToken_bearerChallenges(t *testing.T) {
	t.Run("separate token server", func(t *testing.T) {
		resetTokenCache(t)

		tokenServer := httpstub.NewTLS(t)
		tokenServer.AppendHandlers(tokenHandler(t))

		server := httpstub.NewTLS(t)
		server.AppendHandlers(challengeHandler(t, http.StatusUnauthorized, bearerChallenge(tokenServer.Addr())))

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(server)), "", server.Client(), registryConfig.Registry{}, "")
		require.NoError(t, err)
		assert.Equal(t, TokenResult{Token: "Bearer mock-token", ChallengeHost: tokenServer.Addr()}, got)
		assert.Len(t, server.ReceivedRequests(), 1)
		assert.Len(t, tokenServer.ReceivedRequests(), 1)
	})

	t.Run("registry issues its own tokens", func(t *testing.T) {
		resetTokenCache(t)

		server := httpstub.NewTLS(t)
		server.RouteToHandler(http.MethodGet, httpstub.Path("/v2/"), challengeHandler(t, http.StatusUnauthorized, bearerChallenge(server.Addr())))
		server.RouteToHandler(http.MethodGet, httpstub.Path("/token"), tokenHandler(t))

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(server)), "", server.Client(), registryConfig.Registry{}, "")
		require.NoError(t, err)
		assert.Equal(t, TokenResult{Token: "Bearer mock-token", ChallengeHost: server.Addr()}, got)
		assert.Len(t, server.ReceivedRequests(), 2)
	})

	t.Run("service derived from the realm", func(t *testing.T) {
		resetTokenCache(t)

		server := httpstub.NewTLS(t)
		challenge := fmt.Sprintf(`Bearer realm="https://%s/token"`, server.Addr())
		server.RouteToHandler(http.MethodGet, httpstub.Path("/v2/"), challengeHandler(t, http.StatusUnauthorized, challenge))
		server.RouteToHandler(http.MethodGet, httpstub.Path("/token"), httpstub.CombineHandlers(
			httpstub.VerifyQuery(t, "scope=repository%3Atest%2Fimage%3Apull&service="+url.QueryEscape(server.Addr())),
			tokenHandler(t),
		))

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(server)), "", server.Client(), registryConfig.Registry{}, "")
		require.NoError(t, err)
		assert.Equal(t, TokenResult{Token: "Bearer mock-token", ChallengeHost: server.Addr()}, got)
		assert.Len(t, server.ReceivedRequests(), 2)
	})

	t.Run("redirected challenge", func(t *testing.T) {
		resetTokenCache(t)

		redirectServer := httpstub.NewTLS(t)
		redirectServer.RouteToHandler(http.MethodGet, httpstub.Path("/v2/"), challengeHandler(t, http.StatusUnauthorized, bearerChallenge(redirectServer.Addr())))
		redirectServer.RouteToHandler(http.MethodGet, httpstub.Path("/token"), tokenHandler(t))

		server := httpstub.NewTLS(t)
		server.AppendHandlers(httpstub.CombineHandlers(
			httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
			httpstub.RespondWith(http.StatusFound, "", http.Header{"Location": {redirectServer.URL() + "/v2/"}}),
		))

		got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(server)), "", server.Client(), registryConfig.Registry{}, "")
		require.NoError(t, err)
		assert.Equal(t, TokenResult{
			Token:         "Bearer mock-token",
			ChallengeHost: redirectServer.Addr(),
			Redirected:    true,
			RedirectHost:  redirectServer.Addr(),
		}, got)
		assert.Len(t, server.ReceivedRequests(), 1)
		assert.Len(t, redirectServer.ReceivedRequests(), 2)
	})
}

// TestGetToken_redirectLimit verifies that the registry client follows at most
// defaultMaxRedirects redirects of a challenge request, and that GetToken then
// reports the last redirect response, which carries no challenge, as an
// unexpected status.
func TestGetToken_redirectLimit(t *testing.T) {
	t.Parallel()

	// The last server has no handler, so a request past the limit fails the test.
	servers := make([]*httpstub.Server, defaultMaxRedirects+1)
	for i := range servers {
		servers[i] = httpstub.New(t)
	}

	for i := range defaultMaxRedirects {
		servers[i].AppendHandlers(httpstub.CombineHandlers(
			httpstub.VerifyRequest(t, http.MethodGet, httpstub.Path("/v2/")),
			httpstub.RespondWith(http.StatusTemporaryRedirect, "", http.Header{"Location": {servers[i+1].URL() + "/v2/"}}),
		))
	}

	settings := registryConfig.Registry{TLSSkip: true}

	got, err := GetToken(newTestLogger(t), t.Context(), newImageContainer(t, registryImage(servers[0])), "", NewAuthClient(nil, settings), settings, "")
	require.ErrorIs(t, err, errUnexpectedStatus)
	assert.Empty(t, got.Token)

	for i := range defaultMaxRedirects {
		assert.Len(t, servers[i].ReceivedRequests(), 1, "server %d", i)
	}
}
