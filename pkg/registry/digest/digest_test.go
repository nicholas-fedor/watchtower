package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerImage "github.com/moby/moby/api/types/image"

	mockAuth "github.com/nicholas-fedor/watchtower/pkg/registry/auth/mocks"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestNormalizeDigest(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeDigest(tt.digest)
			assert.Equal(t, tt.expected, result, "NormalizeDigest(%q)", tt.digest)
		})
	}
}

func TestDigestsMatch(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DigestsMatch(tt.localDigests, tt.remoteDigest)
			assert.Equal(t, tt.expected, result, "DigestsMatch(%v, %q)", tt.localDigests, tt.remoteDigest)
		})
	}
}

func TestFormatDigest(t *testing.T) {
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
			assert.Equal(t, tt.want, FormatDigest(tt.input))
		})
	}
}

func TestCompareDigestWithRemote(t *testing.T) {
	const remoteHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	mc := mockTypes.NewMockContainer(t)
	mc.On("Name").Return("outdated")
	mc.On("HasImageInfo").Return(true)
	mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
		RepoDigests: []string{
			"registry.example.com/myapp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:"+remoteHash)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parts := strings.SplitN(server.URL, "://", 2)
	host := parts[len(parts)-1]
	mc.On("ImageName").Return(host + "/myapp:latest")

	match, remoteDigest, err := CompareDigestWithRemote(context.Background(), mc, "", server.URL)
	require.NoError(t, err)
	assert.False(t, match)
	assert.Equal(t, "sha256:"+remoteHash, remoteDigest)
}

func TestCompareDigest(t *testing.T) {
	tests := []struct {
		name           string
		setupContainer func(t *testing.T) *mockTypes.MockContainer
		setupServer    func() *httptest.Server
		expected       bool
		wantErr        bool
	}{
		{
			name: "returns error when container has no image info",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("test-container")
				mc.On("ImageName").Return("nginx:latest")
				mc.On("HasImageInfo").Return(false)

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Errorf("server should not be called when image info is missing")
				}))
			},
			expected: false,
			wantErr:  true,
		},
		{
			name: "returns true for locally built image with empty RepoDigests",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("local-image")
				mc.On("ImageName").Return("local-image:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{RepoDigests: []string{}})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Errorf("server should not be called for locally built image")
				}))
			},
			expected: true,
			wantErr:  false,
		},
		{
			name: "matching digests via HEAD request",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("up-to-date")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Docker-Content-Digest", "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
					w.WriteHeader(http.StatusOK)
				}))
			},
			expected: true,
			wantErr:  false,
		},
		{
			name: "non-matching digests via HEAD request",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("outdated")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Docker-Content-Digest", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
					w.WriteHeader(http.StatusOK)
				}))
			},
			expected: false,
			wantErr:  false,
		},
		{
			name: "falls back to GET when HEAD returns non-2xx without 404",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("fallback-test")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusInternalServerError)

						return
					}

					w.Header().Set("Docker-Content-Digest", "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
					w.WriteHeader(http.StatusOK)
				}))
			},
			expected: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := tt.setupContainer(t)

			server := tt.setupServer()
			defer server.Close()

			serverURL := server.URL
			parts := strings.SplitN(serverURL, "://", 2)
			host := parts[len(parts)-1]

			container.On("ImageName").Return(host + "/myapp:latest").Maybe()

			got, err := CompareDigest(context.Background(), container, "", serverURL)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.expected, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFetchDigest(t *testing.T) {
	tests := []struct {
		name           string
		setupContainer func(t *testing.T) *mockTypes.MockContainer
		setupServer    func() *httptest.Server
		expected       string
		wantErr        bool
	}{
		{
			name: "extracts digest from GET response header",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("fetch-header")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Docker-Content-Digest", "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
					w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}`))
				}))
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "extracts digest from GET response JSON body",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("fetch-json-body")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
					w.WriteHeader(http.StatusOK)

					body, err := json.Marshal(map[string]string{"digest": "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"})
					if err != nil {
						panic(err)
					}

					_, _ = w.Write(body)
				}))
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "extracts digest from plain text body",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("fetch-plain-text")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"))
				}))
			},
			expected: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr:  false,
		},
		{
			name: "returns error when no digest header and empty body",
			setupContainer: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("fetch-empty-body")
				mc.On("ImageName").Return("registry.example.com/myapp:latest")
				mc.On("HasImageInfo").Return(true)
				mc.On("ImageInfo").Return(&dockerImage.InspectResponse{
					RepoDigests: []string{
						"registry.example.com/myapp@sha256:abc",
					},
				})

				return mc
			},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := tt.setupContainer(t)

			server := tt.setupServer()
			defer server.Close()

			parts := strings.SplitN(server.URL, "://", 2)
			host := parts[len(parts)-1]
			container.On("ImageName").Return(host + "/myapp:latest").Maybe()

			got, err := FetchDigest(context.Background(), container, "", server.URL)
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

func TestBuildManifestURL(t *testing.T) {
	tests := []struct {
		name         string
		setupViper   func()
		container    func(t *testing.T) *mockTypes.MockContainer
		hostOverride string
		expectedURL  string
		expectedHost string
		wantErr      bool
	}{
		{
			name: "builds HTTPS manifest URL with empty override",
			setupViper: func() {
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			},
			container: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("https-default")
				mc.On("ImageName").Return("nginx:latest")

				return mc
			},
			hostOverride: "",
			expectedURL:  "https://index.docker.io/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
			wantErr:      false,
		},
		{
			name: "builds HTTP manifest URL when TLS is skipped",
			setupViper: func() {
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			},
			container: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("http-tls-skip")
				mc.On("ImageName").Return("nginx:latest")

				return mc
			},
			hostOverride: "",
			expectedURL:  "http://index.docker.io/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
			wantErr:      false,
		},
		{
			name: "overrides host with bare host string",
			setupViper: func() {
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			},
			container: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("bare-host-override")
				mc.On("ImageName").Return("nginx:latest")

				return mc
			},
			hostOverride: "mirror.example.com",
			expectedURL:  "https://mirror.example.com/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
			wantErr:      false,
		},
		{
			name: "overrides host and scheme from full URL",
			setupViper: func() {
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			},
			container: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("full-url-override")
				mc.On("ImageName").Return("nginx:latest")

				return mc
			},
			hostOverride: "http://mirror.example.com:5000",
			expectedURL:  "http://mirror.example.com:5000/v2/library/nginx/manifests/latest",
			expectedHost: "index.docker.io",
			wantErr:      false,
		},
		{
			name: "redirects lscr.io to ghcr.io",
			setupViper: func() {
				viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
			},
			container: func(t *testing.T) *mockTypes.MockContainer {
				t.Helper()
				mc := mockTypes.NewMockContainer(t)
				mc.On("Name").Return("lscr-redirect")
				mc.On("ImageName").Return("lscr.io/linuxserver/radarr:latest")

				return mc
			},
			hostOverride: "",
			expectedURL:  "https://ghcr.io/v2/linuxserver/radarr/manifests/latest",
			expectedHost: "lscr.io",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupViper()

			gotURL, gotHost, _, err := BuildManifestURL(tt.container(t), tt.hostOverride)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedURL, gotURL)
			assert.Equal(t, tt.expectedHost, gotHost)
		})
	}
}

func TestHandleManifestResponse(t *testing.T) {
	tests := []struct {
		name           string
		setupResponse  func() *http.Response
		method         string
		originalHost   string
		challengeHost  string
		redirected     bool
		parsedURL      *url.URL
		currentHost    string
		expectedDigest string
		expectedURL    string
		expectedRetry  bool
		wantErr        bool
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
			originalHost:  "registry.example.com",
			challengeHost: "",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:   "registry.example.com",
			wantErr:       true,
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
			challengeHost:  "",
			redirected:     false,
			parsedURL:      mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:    "registry.example.com",
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
			originalHost:  "registry.example.com",
			challengeHost: "",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:   "registry.example.com",
			wantErr:       true,
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
			originalHost:  "registry.example.com",
			challengeHost: "",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:   "registry.example.com",
			wantErr:       true,
		},
		{
			name:   "non-redirected HEAD 401 retries on challenge host",
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
			challengeHost: "ghcr.io",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:   "registry.example.com",
			expectedURL:   "https://ghcr.io/v2/manifests/latest",
			expectedRetry: true,
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
			challengeHost: "",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:   "registry.example.com",
			expectedURL:   "https://new-registry.example.com/v2/manifests/latest",
			expectedRetry: true,
			wantErr:       false,
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
					Request:    &http.Request{URL: mustParseURL("https://mirror.example.com/v2/manifests/latest")},
				}
			},
			originalHost:  "registry.example.com",
			challengeHost: "",
			redirected:    false,
			parsedURL:     mustParseURL("https://registry.example.com/v2/manifests/latest"),
			currentHost:   "mirror.example.com",
			expectedURL:   "https://registry.example.com/v2/manifests/latest",
			expectedRetry: true,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.setupResponse()
			if resp.Body != nil {
				defer resp.Body.Close()
			}

			gotDigest, gotURL, gotRetry, err := HandleManifestResponse(
				resp,
				tt.method,
				tt.originalHost,
				tt.challengeHost,
				tt.redirected,
				tt.parsedURL,
				tt.currentHost,
			)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedDigest, gotDigest)
			assert.Equal(t, tt.expectedURL, gotURL)
			assert.Equal(t, tt.expectedRetry, gotRetry)
		})
	}
}

func TestExtractHeadDigest(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.setupResp()
			if resp.Body != nil {
				defer resp.Body.Close()
			}

			got, err := ExtractHeadDigest(resp)
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

func TestExtractGetDigest(t *testing.T) {
	tests := []struct {
		name      string
		setupResp func() *http.Response
		expected  string
		wantErr   bool
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
					Body:       &errReadCloser{},
				}
			},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.setupResp()
			if resp.Body != nil {
				defer resp.Body.Close()
			}

			got, err := ExtractGetDigest(resp)
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

// errReadCloser is an io.ReadCloser that always returns an error.
type errReadCloser struct{}

func (e *errReadCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read error")
}

func (e *errReadCloser) Close() error {
	return nil
}

func TestMakeManifestRequest(t *testing.T) {
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
			ctx := context.Background()
			req, err := makeManifestRequest(ctx, tt.method, tt.manifestURL, tt.token)
			tt.verify(t, req, err)
		})
	}
}

func TestRetryManifestRequest(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		updatedURL  string
		token       string
		setupClient func(t *testing.T) *mockAuth.MockClient
		expected    string
		wantErr     bool
	}{
		{
			name:       "returns digest from successful retry response",
			method:     http.MethodHead,
			updatedURL: "https://registry.example.com/v2/manifests/latest",
			token:      "Bearer token123",
			setupClient: func(t *testing.T) *mockAuth.MockClient {
				t.Helper()
				mc := mockAuth.NewMockClient(t)
				mc.On("Do", mock.Anything).Return(&http.Response{
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
				mc.On("Do", mock.Anything).Return(nil, errors.New("connection refused"))

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
				mc.On("Do", mock.Anything).Return(&http.Response{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient(t)

			got, err := retryManifestRequest(
				context.Background(),
				tt.method,
				tt.updatedURL,
				tt.token,
				"registry.example.com",
				"ghcr.io",
				false,
				mustParseURL("https://registry.example.com/v2/manifests/latest"),
				client,
			)
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

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("failed to parse URL %q: %v", raw, err))
	}

	return u
}
