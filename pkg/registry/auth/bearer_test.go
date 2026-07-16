package auth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/distribution/reference"
	"github.com/maypok86/otter/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockAuth "github.com/nicholas-fedor/watchtower/pkg/registry/auth/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func Test_tokenExpiryCalculator_ExpireAfterCreate(t *testing.T) {
	calc := &tokenExpiryCalculator{}
	entry := otter.Entry[string, tokenCacheEntry]{
		Key:   "test",
		Value: tokenCacheEntry{expiresAt: time.Now().Add(5 * time.Minute)},
	}

	got := calc.ExpireAfterCreate(entry)
	assert.InDelta(t, 5*time.Minute.Seconds(), got.Seconds(), 1)
}

func Test_tokenExpiryCalculator_ExpireAfterUpdate(t *testing.T) {
	calc := &tokenExpiryCalculator{}
	entry := otter.Entry[string, tokenCacheEntry]{
		Key:   "test",
		Value: tokenCacheEntry{expiresAt: time.Now().Add(10 * time.Minute)},
	}

	got := calc.ExpireAfterUpdate(entry, tokenCacheEntry{})
	assert.InDelta(t, 10*time.Minute.Seconds(), got.Seconds(), 1)
}

func Test_tokenExpiryCalculator_ExpireAfterRead(t *testing.T) {
	calc := &tokenExpiryCalculator{}
	entry := otter.Entry[string, tokenCacheEntry]{
		Key:               "test",
		ExpiresAtNano:     time.Now().Add(3 * time.Minute).UnixNano(),
		RefreshableAtNano: time.Now().Add(24 * time.Hour).UnixNano(),
		SnapshotAtNano:    time.Now().UnixNano(),
	}

	got := calc.ExpireAfterRead(entry)
	assert.Equal(t, entry.ExpiresAfter(), got)
}

func Test_initTokenCache(t *testing.T) {
	assert.NotPanics(t, func() {
		initTokenCache()
	})
}

func Test_computeTokenExpiry(t *testing.T) {
	now := time.Now()
	truncatedToSeconds := now.Truncate(time.Second)

	tests := []struct {
		name string
		args *types.TokenResponse
		want time.Time
	}{
		{
			name: "uses expires_in when provided",
			args: &types.TokenResponse{
				ExpiresIn: 3600,
			},
			want: truncatedToSeconds.Add(3600 * time.Second),
		},
		{
			name: "falls back to default TTL when expires_in is negative",
			args: &types.TokenResponse{
				ExpiresIn: -1,
			},
			want: truncatedToSeconds.Add(defaultTokenTTL),
		},
		{
			name: "falls back to issued_at when expires_in is zero",
			args: &types.TokenResponse{
				IssuedAt: now.Truncate(time.Second).Format(time.RFC3339),
			},
			want: truncatedToSeconds.Add(defaultTokenTTL),
		},
		{
			name: "falls back to default TTL when both are missing",
			args: &types.TokenResponse{},
			want: truncatedToSeconds.Add(defaultTokenTTL),
		},
		{
			name: "falls back to default TTL when issued_at is invalid",
			args: &types.TokenResponse{
				IssuedAt: "not-a-date",
			},
			want: truncatedToSeconds.Add(defaultTokenTTL),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeTokenExpiry(tt.args)
			assert.InDelta(t, tt.want.Unix(), got.Unix(), 2)
		})
	}
}

func Test_readBearerTokenWithExpiry(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		image   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid token response",
			body:    `{"token":"test-token","expires_in":3600,"issued_at":"2024-01-01T00:00:00Z"}`,
			image:   "test/image",
			want:    "test-token",
			wantErr: false,
		},
		{
			name:    "invalid JSON returns error",
			body:    `{invalid json}`,
			image:   "test/image",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty token returns empty string",
			body:    `{"token":""}`,
			image:   "test/image",
			want:    "",
			wantErr: false,
		},
		{
			name:    "missing token field returns empty string",
			body:    `{"expires_in":3600}`,
			image:   "test/image",
			want:    "",
			wantErr: false,
		},
		{
			name:    "access_token used when token is empty",
			body:    `{"access_token":"fallback-token","expires_in":3600}`,
			image:   "test/image",
			want:    "fallback-token",
			wantErr: false,
		},
		{
			name:    "token takes precedence over access_token",
			body:    `{"token":"primary-token","access_token":"fallback-token","expires_in":3600}`,
			image:   "test/image",
			want:    "primary-token",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := readBearerTokenWithExpiry(strings.NewReader(tt.body), tt.image)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_addBasicAuth(t *testing.T) {
	tests := []struct {
		name         string
		request      *http.Request
		imageName    string
		registryAuth string
		wantHeader   string
	}{
		{
			name:         "adds basic auth header when credentials provided",
			request:      mustNewRequest(t, "http://example.com"),
			imageName:    "test/image",
			registryAuth: "dGVzdHVzZXI6dGVzdHBhc3M=",
			wantHeader:   "Basic dGVzdHVzZXI6dGVzdHBhc3M=",
		},
		{
			name:         "does not add header when no credentials",
			request:      mustNewRequest(t, "http://example.com"),
			imageName:    "test/image",
			registryAuth: "",
			wantHeader:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addBasicAuth(tt.request, tt.imageName, tt.registryAuth)
			assert.Equal(t, tt.wantHeader, tt.request.Header.Get("Authorization"))
		})
	}
}

func backgroundContext() context.Context {
	return context.Background()
}

func emptyContext() context.Context {
	return nil
}

func Test_newBearerRequest(t *testing.T) {
	authURL, _ := url.Parse("https://example.com/token")

	tests := []struct {
		name      string
		authURL   *url.URL
		imageName string
		ctx       func() context.Context
		wantErr   bool
	}{
		{
			name:      "creates GET request with context",
			authURL:   authURL,
			imageName: "test/image",
			ctx:       backgroundContext,
			wantErr:   false,
		},
		{
			name:      "nil context returns error",
			authURL:   authURL,
			imageName: "test/image",
			ctx:       emptyContext,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newBearerRequest(tt.ctx(), tt.authURL, tt.imageName)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, http.MethodGet, got.Method)
			assert.Equal(t, "https://example.com/token", got.URL.String())
		})
	}
}

func Test_resolveService(t *testing.T) {
	tests := []struct {
		name      string
		values    challengeValues
		image     string
		challenge string
		want      string
	}{
		{
			name:      "returns service when present",
			values:    challengeValues{service: "ghcr.io"},
			image:     "test/image",
			challenge: "bearer realm=\"https://ghcr.io/token\",service=\"ghcr.io\"",
			want:      "ghcr.io",
		},
		{
			name:      "derives service from realm when service is empty",
			values:    challengeValues{realm: "https://ghcr.io/token"},
			image:     "test/image",
			challenge: "bearer realm=\"https://ghcr.io/token\"",
			want:      "ghcr.io",
		},
		{
			name:      "returns empty when both service and realm are empty",
			values:    challengeValues{},
			image:     "test/image",
			challenge: "bearer",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveService(tt.values, tt.image, tt.challenge)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_validateRequiredChallengeValues(t *testing.T) {
	tests := []struct {
		name      string
		values    challengeValues
		image     string
		challenge string
		wantErr   bool
	}{
		{
			name:      "valid realm and service returns no error",
			values:    challengeValues{realm: "https://ghcr.io/token", service: "ghcr.io"},
			image:     "test/image",
			challenge: "bearer",
			wantErr:   false,
		},
		{
			name:      "missing realm returns error",
			values:    challengeValues{service: "ghcr.io"},
			image:     "test/image",
			challenge: "bearer",
			wantErr:   true,
		},
		{
			name:      "missing service returns error",
			values:    challengeValues{realm: "https://ghcr.io/token"},
			image:     "test/image",
			challenge: "bearer",
			wantErr:   true,
		},
		{
			name:      "missing both returns error",
			values:    challengeValues{},
			image:     "test/image",
			challenge: "bearer",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequiredChallengeValues(tt.values, tt.image, tt.challenge)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

func Test_buildAuthQuery(t *testing.T) {
	authURL, _ := url.Parse("https://ghcr.io/token")
	imageRef, _ := reference.ParseNormalizedNamed("ghcr.io/user/repo:latest")

	tests := []struct {
		name     string
		authURL  *url.URL
		values   challengeValues
		imageRef reference.Named
		want     string
	}{
		{
			name:     "adds service and scope query params",
			authURL:  authURL,
			values:   challengeValues{service: "ghcr.io", scope: ""},
			imageRef: imageRef,
			want:     "scope=repository%3Auser%2Frepo%3Apull&service=ghcr.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAuthQuery(tt.authURL, tt.values, tt.imageRef)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.RawQuery)
		})
	}
}

func TestGetAuthURL(t *testing.T) {
	imageRef, _ := reference.ParseNormalizedNamed("ghcr.io/user/repo:latest")

	tests := []struct {
		name      string
		challenge string
		imageRef  reference.Named
		wantErr   bool
		wantHost  string
		wantQuery string
	}{
		{
			name:      "valid challenge constructs auth URL",
			challenge: `bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/repo:pull"`,
			imageRef:  imageRef,
			wantErr:   false,
			wantHost:  "ghcr.io",
			wantQuery: "scope=repository%3Auser%2Frepo%3Apull&service=ghcr.io",
		},
		{
			name:      "missing realm returns error",
			challenge: `bearer service="ghcr.io"`,
			imageRef:  imageRef,
			wantErr:   true,
		},
		{
			name:      "realm without scheme returns error",
			challenge: `bearer realm="example.com/token",service="ghcr.io"`,
			imageRef:  imageRef,
			wantErr:   true,
		},
		{
			name:      "realm with non-HTTP(S) scheme returns error",
			challenge: `bearer realm="ftp://example.com/token",service="ghcr.io"`,
			imageRef:  imageRef,
			wantErr:   true,
		},
		{
			name:      "realm with empty host returns error",
			challenge: `bearer realm="http:///token",service="ghcr.io"`,
			imageRef:  imageRef,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAuthURL(tt.challenge, tt.imageRef)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, got.Host)
			assert.Equal(t, tt.wantQuery, got.RawQuery)
		})
	}
}

func TestGetBearerToken(t *testing.T) {
	ctx := context.Background()
	imageRef, _ := reference.ParseNormalizedNamed("test/image:latest")
	mockClient := mockAuth.NewMockClient(t)

	tests := []struct {
		name         string
		challenge    string
		imageRef     reference.Named
		registryAuth string
		client       Client
		onDo         *http.Response
		doErr        error
		want         string
		wantErr      bool
	}{
		{
			name:      "successful token fetch",
			challenge: `bearer realm="https://test.com/token",service="test.com"`,
			imageRef:  imageRef,
			onDo: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"token":"test-token","expires_in":3600}`)),
			},
			want:    "Bearer test-token",
			wantErr: false,
		},
		{
			name:      "client Do returns error",
			challenge: `bearer realm="https://error.test.com/token",service="error.test.com"`,
			imageRef:  imageRef,
			doErr:     assert.AnError,
			want:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.onDo != nil || tt.doErr != nil {
				mockClient.On("Do", mock.Anything).Return(tt.onDo, tt.doErr).Once()
			}

			got, err := GetBearerToken(ctx, tt.challenge, tt.imageRef, tt.registryAuth, mockClient)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_executeBearerTokenRequest_cacheMiss(t *testing.T) {
	ctx := context.Background()
	authURL, _ := url.Parse("https://test.com/token?service=test.com")
	imageName := "test/image"
	mockClient := mockAuth.NewMockClient(t)

	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"token":"cached-token","expires_in":3600}`)),
	}, nil).Once()

	got, err := executeBearerTokenRequest(ctx, authURL, imageName, "", mockClient)
	require.NoError(t, err)
	assert.Equal(t, "Bearer cached-token", got)
}

func Test_executeBearerTokenRequest_cacheHit(t *testing.T) {
	ctx := context.Background()
	authURL, _ := url.Parse("https://test.com/token?service=test.com")
	imageName := "test/image"
	mockClient := mockAuth.NewMockClient(t)

	initTokenCache()

	cacheKey := authURL.String() + "|"
	tokenCache.SetIfAbsent(cacheKey, tokenCacheEntry{
		token:     "Bearer cached-token",
		expiresAt: time.Now().Add(time.Hour),
	})

	got, err := executeBearerTokenRequest(ctx, authURL, imageName, "", mockClient)
	require.NoError(t, err)
	assert.Equal(t, "Bearer cached-token", got)
}

func Test_performBearerTokenFetch(t *testing.T) {
	ctx := context.Background()
	authURL, _ := url.Parse("https://test.com/token?service=test.com")
	mockClient := mockAuth.NewMockClient(t)

	tests := []struct {
		name      string
		authURL   *url.URL
		imageName string
		auth      string
		onDo      *http.Response
		doErr     error
		want      string
		wantErr   bool
	}{
		{
			name:      "successful token fetch",
			authURL:   authURL,
			imageName: "test/image",
			auth:      "",
			onDo: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"token":"test-token","expires_in":1800,"issued_at":"2024-01-01T00:00:00Z"}`)),
			},
			want:    "test-token",
			wantErr: false,
		},
		{
			name:      "client error returns error",
			authURL:   authURL,
			imageName: "test/image",
			auth:      "",
			doErr:     assert.AnError,
			want:      "",
			wantErr:   true,
		},
		{
			name:      "401 Unauthorized returns error",
			authURL:   authURL,
			imageName: "test/image",
			auth:      "",
			onDo: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid credentials"}`)),
			},
			want:    "",
			wantErr: true,
		},
		{
			name:      "429 Too Many Requests returns error",
			authURL:   authURL,
			imageName: "test/image",
			auth:      "",
			onDo: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate limit"}`)),
			},
			want:    "",
			wantErr: true,
		},
		{
			name:      "500 Internal Server Error returns error",
			authURL:   authURL,
			imageName: "test/image",
			auth:      "",
			onDo: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"server error"}`)),
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.onDo != nil || tt.doErr != nil {
				mockClient.On("Do", mock.Anything).Return(tt.onDo, tt.doErr).Once()
			}

			got, _, err := performBearerTokenFetch(ctx, tt.authURL, tt.imageName, tt.auth, mockClient)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
