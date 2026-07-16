package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockAuth "github.com/nicholas-fedor/watchtower/pkg/registry/auth/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func Test_resolveChallengeScheme(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "returns https for empty host",
			host: "",
			want: "https",
		},
		{
			name: "preserves https scheme by default",
			host: "https://example.com",
			want: "https",
		},
		{
			name: "returns https for non-empty host without explicit scheme",
			host: "example.com",
			want: "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveChallengeScheme(tt.host)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_resolveEndpointHost(t *testing.T) {
	tests := []struct {
		name      string
		endpoint  string
		canonical string
		scheme    string
		want      string
		want1     string
	}{
		{
			name:      "returns endpoint and http when endpoint is provided",
			endpoint:  "http://custom.example.com",
			canonical: "index.docker.io",
			scheme:    "https",
			want:      "custom.example.com",
			want1:     "http",
		},
		{
			name:      "returns canonical host and https when endpoint is empty",
			endpoint:  "",
			canonical: "index.docker.io",
			scheme:    "https",
			want:      "index.docker.io",
			want1:     "https",
		},
		{
			name:      "returns empty host and https when both are empty",
			endpoint:  "",
			canonical: "",
			scheme:    "https",
			want:      "",
			want1:     "https",
		},
		{
			name:      "bare endpoint preserves provided scheme",
			endpoint:  "custom.example.com",
			canonical: "index.docker.io",
			scheme:    "http",
			want:      "custom.example.com",
			want1:     "http",
		},
		{
			name:      "bare endpoint falls back to https when scheme is https",
			endpoint:  "custom.example.com",
			canonical: "index.docker.io",
			scheme:    "https",
			want:      "custom.example.com",
			want1:     "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := resolveEndpointHost(tt.endpoint, tt.canonical, tt.scheme)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want1, got1)
		})
	}
}

func TestGetChallengeURL(t *testing.T) {
	tests := []struct {
		name       string
		imageRef   string
		endpoint   string
		wantHost   string
		wantScheme string
		wantErr    bool
	}{
		{
			name:       "docker.io maps to index.docker.io",
			imageRef:   "docker.io/library/nginx:latest",
			endpoint:   "",
			wantHost:   "index.docker.io",
			wantScheme: "https",
			wantErr:    false,
		},
		{
			name:       "custom endpoint overrides host",
			imageRef:   "docker.io/library/nginx:latest",
			endpoint:   "mirror.example.com",
			wantHost:   "mirror.example.com",
			wantScheme: "https",
			wantErr:    false,
		},
		{
			name:       "lscr.io maps to ghcr.io",
			imageRef:   "lscr.io/linuxserver/nginx:latest",
			endpoint:   "",
			wantHost:   "ghcr.io",
			wantScheme: "https",
			wantErr:    false,
		},
		{
			name:       "non-docker host passes through",
			imageRef:   "ghcr.io/example/app:latest",
			endpoint:   "",
			wantHost:   "ghcr.io",
			wantScheme: "https",
			wantErr:    false,
		},
		{
			name:       "localhost passes through",
			imageRef:   "localhost:5000/myimage:latest",
			endpoint:   "",
			wantHost:   "localhost:5000",
			wantScheme: "https",
			wantErr:    false,
		},
		{
			name:     "invalid image ref returns error",
			imageRef: ":::invalid:::",
			endpoint: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := reference.ParseNormalizedNamed(tt.imageRef)
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected parse error: %v", err)
				}

				return
			}

			got := GetChallengeURL(ref, tt.endpoint)
			if tt.wantErr {
				assert.Empty(t, got.Host)

				return
			}

			assert.Equal(t, tt.wantHost, got.Host)
			assert.Equal(t, tt.wantScheme, got.Scheme)
			assert.Equal(t, "/v2/", got.Path)
		})
	}
}

func TestGetChallengeRequest(t *testing.T) {
	server := newTestServer(t, http.StatusOK, "")
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)

	tests := []struct {
		name    string
		url     url.URL
		wantErr bool
	}{
		{
			name:    "creates GET request with context",
			url:     *serverURL,
			wantErr: false,
		},
		{
			name:    "empty URL still creates request",
			url:     url.URL{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetChallengeRequest(context.Background(), tt.url)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, http.MethodGet, got.Method)
		})
	}
}

func Test_handleSuccessfulChallenge(t *testing.T) {
	tests := []struct {
		name         string
		redirected   bool
		redirectHost string
		want         TokenResult
	}{
		{
			name:         "non-redirected response",
			redirected:   false,
			redirectHost: "",
			want: TokenResult{
				Token:         "",
				ChallengeHost: "",
				Redirected:    false,
				RedirectHost:  "",
			},
		},
		{
			name:         "redirected response",
			redirected:   true,
			redirectHost: "new-host.example.com",
			want: TokenResult{
				Token:         "",
				ChallengeHost: "",
				Redirected:    true,
				RedirectHost:  "new-host.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handleSuccessfulChallenge(tt.redirected, tt.redirectHost)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_handleEmptyAuthHeader(t *testing.T) {
	ctx := context.Background()
	mockContainer := mockTypes.NewMockContainer(t)
	mockClient := mockAuth.NewMockClient(t)

	tests := []struct {
		name         string
		response     challengeResponse
		registryAuth string
		wantErr      error
	}{
		{
			name: "401 without WWW-Authenticate returns no credentials error",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: "",
				redirected:    false,
				redirectHost:  "",
			},
			registryAuth: "",
			wantErr:      errNoCredentials,
		},
		{
			name: "non-200 without WWW-Authenticate returns unexpected status error",
			response: challengeResponse{
				statusCode:    http.StatusInternalServerError,
				wwwAuthHeader: "",
				redirected:    false,
				redirectHost:  "",
			},
			registryAuth: "",
			wantErr:      errUnexpectedStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := processChallengeResponse(
				ctx,
				mockContainer,
				tt.registryAuth,
				mockClient,
				tt.response.redirected,
				tt.response.redirectHost,
				"",
				logrus.Fields{},
				tt.response,
			)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)

				return
			}

			t.Fatal("expected error")
		})
	}
}

func Test_handleBasicAuthChallenge(t *testing.T) {
	tests := []struct {
		name         string
		registryAuth string
		fields       logrus.Fields
		redirected   bool
		redirectHost string
		originalHost string
		want         TokenResult
		wantErr      bool
	}{
		{
			name:         "valid registry auth returns token result",
			registryAuth: "dGVzdA==",
			fields:       logrus.Fields{},
			redirected:   false,
			redirectHost: "",
			originalHost: "registry.example.com",
			want: TokenResult{
				Token:         "Basic dGVzdA==",
				ChallengeHost: "",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
		},
		{
			name:         "empty registry auth returns error",
			registryAuth: "",
			fields:       logrus.Fields{},
			redirected:   false,
			redirectHost: "",
			originalHost: "registry.example.com",
			want:         TokenResult{},
			wantErr:      true,
		},
		{
			name:         "cross-origin redirect returns error",
			registryAuth: "dGVzdA==",
			fields:       logrus.Fields{},
			redirected:   true,
			redirectHost: "evil.example.com",
			originalHost: "registry.example.com",
			want:         TokenResult{},
			wantErr:      true,
		},
		{
			name:         "same-host redirect returns token result",
			registryAuth: "dGVzdA==",
			fields:       logrus.Fields{},
			redirected:   true,
			redirectHost: "registry.example.com",
			originalHost: "registry.example.com",
			want: TokenResult{
				Token:         "Basic dGVzdA==",
				ChallengeHost: "",
				Redirected:    true,
				RedirectHost:  "registry.example.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleBasicAuthChallenge(tt.registryAuth, tt.fields, tt.redirected, tt.redirectHost, tt.originalHost)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_handleUnsupportedChallenge(t *testing.T) {
	tests := []struct {
		name      string
		challenge string
		fields    logrus.Fields
		wantErr   bool
	}{
		{
			name:      "basic challenge returns error",
			challenge: "basic realm=\"test\"",
			fields:    logrus.Fields{},
			wantErr:   true,
		},
		{
			name:      "empty challenge returns error",
			challenge: "",
			fields:    logrus.Fields{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleUnsupportedChallenge(tt.challenge, tt.fields)
			require.Error(t, err)
			assert.ErrorIs(t, err, errUnsupportedChallenge)
		})
	}
}

func Test_processChallengeResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     challengeResponse
		registryAuth string
		originalHost string
		setupMock    func(*mockAuth.MockClient)
		want         TokenResult
		wantErr      bool
		errContains  string
	}{
		{
			name: "401 with bearer challenge routes to bearer auth",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Bearer realm="https://challenge.test.com/token",service="challenge.test.com"`,
			},
			registryAuth: "",
			originalHost: "challenge.test.com",
			setupMock: func(mockClient *mockAuth.MockClient) {
				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"test-token"}`)),
				}, nil).Once()
			},
			want: TokenResult{
				Token:         "Bearer test-token",
				ChallengeHost: "challenge.test.com",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
		},
		{
			name: "200 with empty auth header returns success",
			response: challengeResponse{
				statusCode:    http.StatusOK,
				wwwAuthHeader: "",
			},
			registryAuth: "",
			originalHost: "",
			setupMock:    func(*mockAuth.MockClient) {},
			want: TokenResult{
				Token:         "",
				ChallengeHost: "",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
		},
		{
			name: "401 with basic auth challenge",
			response: challengeResponse{
				statusCode:    http.StatusUnauthorized,
				wwwAuthHeader: `Basic realm="test"`,
			},
			registryAuth: "dGVzdA==",
			originalHost: "registry.example.com",
			setupMock:    func(*mockAuth.MockClient) {},
			want: TokenResult{
				Token:         "Basic dGVzdA==",
				ChallengeHost: "",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
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
			setupMock:    func(*mockAuth.MockClient) {},
			want:         TokenResult{},
			wantErr:      true,
			errContains:  "cross-origin redirect not allowed for basic auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mockAuth.NewMockClient(t)
			if tt.setupMock != nil {
				tt.setupMock(mockClient)
			}

			mockContainer := mockTypes.NewMockContainer(t)
			mockContainer.On("ImageName").Return("test/image").Maybe()
			mockContainer.On("IsNoPull", mock.Anything).Return(false).Maybe()
			mockContainer.On("IsStale").Return(false).Maybe()

			fields := logrus.Fields{"image": "test/image"}
			ctx := context.Background()

			got, err := processChallengeResponse(ctx, mockContainer, tt.registryAuth, mockClient, tt.response.redirected, tt.response.redirectHost, tt.originalHost, fields, tt.response)
			if tt.wantErr {
				assert.Error(t, err)

				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_handleBearerAuth(t *testing.T) {
	tests := []struct {
		name          string
		wwwAuthHeader string
		registryAuth  string
		setupMock     func(*mockAuth.MockClient)
		want          TokenResult
		wantErr       bool
		errContains   string
	}{
		{
			name:          "successful bearer auth",
			wwwAuthHeader: `Bearer realm="https://success.test.com/token",service="success.test.com"`,
			registryAuth:  "",
			setupMock: func(mockClient *mockAuth.MockClient) {
				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"test-token"}`)),
				}, nil).Once()
			},
			want: TokenResult{
				Token:         "Bearer test-token",
				ChallengeHost: "success.test.com",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
		},
		{
			name:          "client Do error returns wrapped error",
			wwwAuthHeader: `Bearer realm="https://error.test.com/token",service="error.test.com"`,
			registryAuth:  "",
			setupMock: func(mockClient *mockAuth.MockClient) {
				mockClient.On("Do", mock.Anything).Return(nil, assert.AnError).Once()
			},
			want:        TokenResult{},
			wantErr:     true,
			errContains: "failed to execute bearer token request",
		},
		{
			name:          "empty token returns error",
			wwwAuthHeader: `Bearer realm="https://empty.test.com/token",service="empty.test.com"`,
			registryAuth:  "",
			setupMock: func(mockClient *mockAuth.MockClient) {
				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":""}`)),
				}, nil).Once()
			},
			want:        TokenResult{},
			wantErr:     true,
			errContains: "empty token in response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mockAuth.NewMockClient(t)
			if tt.setupMock != nil {
				tt.setupMock(mockClient)
			}

			mockContainer := mockTypes.NewMockContainer(t)
			mockContainer.On("ImageName").Return("test/image").Maybe()
			mockContainer.On("IsNoPull", mock.Anything).Return(false).Maybe()
			mockContainer.On("IsStale").Return(false).Maybe()

			fields := logrus.Fields{"image": "test/image"}
			ctx := context.Background()

			got, err := handleBearerAuth(ctx, tt.wwwAuthHeader, mockContainer, tt.registryAuth, mockClient, false, "", fields)
			if tt.wantErr {
				assert.Error(t, err)

				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetToken(t *testing.T) {
	tests := []struct {
		name         string
		container    types.Container
		registryAuth string
		endpoint     string
		setupMock    func(*mockAuth.MockClient)
		want         TokenResult
		wantErr      bool
		errContains  string
	}{
		{
			name:         "no-pull container returns error on empty image name",
			container:    newNoPullContainer(t),
			registryAuth: "",
			endpoint:     "",
			setupMock:    func(*mockAuth.MockClient) {},
			want:         TokenResult{},
			wantErr:      true,
			errContains:  "failed to parse image name",
		},
		{
			name:         "localhost registry returns success with empty token",
			container:    newLocalhostContainer(t),
			registryAuth: "",
			endpoint:     "",
			setupMock: func(mockClient *mockAuth.MockClient) {
				challengeURL, _ := url.Parse("https://localhost:5000/v2/")
				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
					Request:    &http.Request{URL: challengeURL},
				}, nil).Once()
			},
			want: TokenResult{
				Token:         "",
				ChallengeHost: "",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
		},
		{
			name:         "successful token fetch",
			container:    newRemoteContainer(t),
			registryAuth: "",
			endpoint:     "",
			setupMock: func(mockClient *mockAuth.MockClient) {
				challengeURL, _ := url.Parse("https://index.docker.io/v2/")
				tokenURL, _ := url.Parse("https://index.docker.io/token?service=index.docker.io&scope=repository%3Atest%2Fimage%3Apull")

				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       http.NoBody,
					Header:     http.Header{"Www-Authenticate": []string{`Bearer realm="https://index.docker.io/token",service="index.docker.io"`}},
					Request:    &http.Request{URL: challengeURL},
				}, nil).Once()

				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"token":"test-token","expires_in":3600}`)),
					Header:     make(http.Header),
					Request:    &http.Request{URL: tokenURL},
				}, nil).Once()
			},
			want: TokenResult{
				Token:         "Bearer test-token",
				ChallengeHost: "index.docker.io",
				Redirected:    false,
				RedirectHost:  "",
			},
			wantErr: false,
		},
		{
			name:         "client Do error returns wrapped error",
			container:    newRemoteContainer(t),
			registryAuth: "",
			endpoint:     "",
			setupMock: func(mockClient *mockAuth.MockClient) {
				challengeURL, _ := url.Parse("https://index.docker.io/v2/")
				mockClient.On("Do", mock.Anything).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
					Request:    &http.Request{URL: challengeURL},
				}, assert.AnError).Once()
			},
			want:        TokenResult{},
			wantErr:     true,
			errContains: "failed to execute challenge request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mockAuth.NewMockClient(t)
			if tt.setupMock != nil {
				tt.setupMock(mockClient)
			}

			ctx := context.Background()

			got, err := GetToken(ctx, tt.container, tt.registryAuth, mockClient, tt.endpoint)
			if tt.wantErr {
				assert.Error(t, err)

				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func newNoPullContainer(t *testing.T) types.Container {
	t.Helper()

	container := mockTypes.NewMockContainer(t)
	container.On("ImageName").Return("").Maybe()
	container.On("IsNoPull", mock.Anything).Return(true).Maybe()
	container.On("IsStale").Return(false).Maybe()
	container.On("IsRunning").Return(true).Maybe()

	return container
}

func newLocalhostContainer(t *testing.T) types.Container {
	t.Helper()

	container := mockTypes.NewMockContainer(t)
	container.On("ImageName").Return("localhost:5000/test/image:latest").Maybe()
	container.On("IsNoPull", mock.Anything).Return(false).Maybe()
	container.On("IsStale").Return(false).Maybe()
	container.On("IsRunning").Return(true).Maybe()

	return container
}

func newRemoteContainer(t *testing.T) types.Container {
	t.Helper()

	container := mockTypes.NewMockContainer(t)
	container.On("ImageName").Return("test/image:latest").Maybe()
	container.On("IsNoPull", mock.Anything).Return(false).Maybe()
	container.On("IsStale").Return(false).Maybe()
	container.On("IsRunning").Return(true).Maybe()

	return container
}

// newTestServer creates an HTTP test server that returns the given status and body.
func newTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)

		if body != "" {
			_, _ = fmt.Fprint(w, body)
		}
	}))
}
