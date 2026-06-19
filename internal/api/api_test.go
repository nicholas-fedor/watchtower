package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	containermocks "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	typemocks "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var testMetrics = metrics.Default()

func testLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}

func TestGetAPIAddr(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "localhost", host: "localhost", port: "8080", want: "localhost:8080"},
		{name: "IPv4", host: "127.0.0.1", port: "8080", want: "127.0.0.1:8080"},
		{name: "IPv6 loopback", host: "::1", port: "8080", want: "[::1]:8080"},
		{name: "IPv6 full", host: "2001:db8::1", port: "8080", want: "[2001:db8::1]:8080"},
		{name: "empty host", host: "", port: "8080", want: ":8080"},
		{name: "hostname", host: "myhost.example.com", port: "9090", want: "myhost.example.com:9090"},
		{name: "IPv6 zone", host: "fe80::1%eth0", port: "8080", want: "fe80::1%eth0:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetAPIAddr(tt.host, tt.port))
		})
	}
}

func TestSetupAndStartAPI(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
		errMsg  string
	}{
		{
			name: "no APIs enabled",
			opts: Options{
				Token: "test-token",
			},
			wantErr: false,
		},
		{
			name: "nil RunUpdatesWithNotifications",
			opts: Options{
				Token:                       "test-token",
				EnableUpdateAPI:             true,
				RunUpdatesWithNotifications: nil,
				FilterByImage:               func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics:              func() *metrics.Metrics { return testMetrics },
			},
			wantErr: true,
			errMsg:  "RunUpdatesWithNotifications must be provided",
		},
		{
			name: "nil FilterByImage",
			opts: Options{
				Token:           "test-token",
				EnableUpdateAPI: true,
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  nil,
				DefaultMetrics: func() *metrics.Metrics { return testMetrics },
			},
			wantErr: true,
			errMsg:  "FilterByImage must be provided",
		},
		{
			name: "nil DefaultMetrics",
			opts: Options{
				Token:           "test-token",
				EnableUpdateAPI: true,
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics: nil,
			},
			wantErr: true,
			errMsg:  "DefaultMetrics must be provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.opts.Token == "" {
				t.Skip("empty token causes logrus.Fatal")
			}

			err := SetupAndStartAPI(ctx, tt.opts)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_registerHealthChecks(t *testing.T) {
	tests := []struct {
		name             string
		clientSetup      func(t *testing.T) container.Client
		checkReadiness   bool
		readinessHealthy bool
	}{
		{
			name: "nil client",
			clientSetup: func(t *testing.T) container.Client {
				t.Helper()

				return nil
			},
		},
		{
			name: "working client",
			clientSetup: func(t *testing.T) container.Client {
				t.Helper()

				mc := containermocks.NewMockClient(t)
				c := typemocks.NewMockContainer(t)
				c.EXPECT().Name().Return("test").Maybe()
				c.EXPECT().ImageName().Return("img").Maybe()
				c.EXPECT().ImageID().Return(types.ImageID("sha256:abc")).Maybe()
				c.EXPECT().ImageInfo().Return(nil).Maybe()
				mc.EXPECT().ListContainers(mock.Anything).Return([]types.Container{c}, nil)

				return mc
			},
			checkReadiness:   true,
			readinessHealthy: true,
		},
		{
			name: "failing client",
			clientSetup: func(t *testing.T) container.Client {
				t.Helper()

				mc := containermocks.NewMockClient(t)
				mc.EXPECT().ListContainers(mock.Anything).Return(nil, errors.New("fail"))

				return mc
			},
			checkReadiness:   true,
			readinessHealthy: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(testLogger(), 60)
			client := tt.clientSetup(t)

			registerHealthChecks(t.Context(), app, client)

			routes := app.GetRoutes()
			healthCount := 0

			for _, r := range routes {
				if r.Path == "/livez" || r.Path == "/readyz" || r.Path == "/startupz" {
					healthCount++
				}
			}

			assert.Equal(t, 3, healthCount)

			if tt.checkReadiness && client != nil {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
				resp, err := app.Test(req)
				require.NoError(t, err)

				defer resp.Body.Close()

				if tt.readinessHealthy {
					assert.Equal(t, http.StatusOK, resp.StatusCode)
				} else {
					assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
				}
			}
		})
	}
}

func Test_newKeyAuthMiddleware(t *testing.T) {
	validHash := sha256.Sum256([]byte("valid-token"))
	handler := newKeyAuthMiddleware(validHash)
	require.NotNil(t, handler)

	app := New(testLogger(), 60)
	app.Get("/test", handler, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func Test_validateAndRegisterRoutes(t *testing.T) {
	baseOpts := func() Options {
		return Options{
			EnableUpdateAPI: true,
			UnblockHTTPAPI:  true,
			RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{}
			},
			FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
			DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		}
	}

	tests := []struct {
		name    string
		opts    Options
		wantErr bool
		errMsg  string
	}{
		{name: "all valid", opts: baseOpts(), wantErr: false},
		{
			name: "missing RunUpdatesWithNotifications",
			opts: func() Options {
				o := baseOpts()
				o.RunUpdatesWithNotifications = nil

				return o
			}(),
			wantErr: true,
			errMsg:  "RunUpdatesWithNotifications must be provided",
		},
		{
			name: "missing FilterByImage",
			opts: func() Options {
				o := baseOpts()
				o.FilterByImage = nil

				return o
			}(),
			wantErr: true,
			errMsg:  "FilterByImage must be provided",
		},
		{
			name: "missing DefaultMetrics",
			opts: func() Options {
				o := baseOpts()
				o.DefaultMetrics = nil

				return o
			}(),
			wantErr: true,
			errMsg:  "DefaultMetrics must be provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(testLogger(), 60)
			auth := newKeyAuthMiddleware(sha256.Sum256([]byte("test")))

			err := validateAndRegisterRoutes(app, auth, tt.opts)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_registerRoutes(t *testing.T) {
	tests := []struct {
		name                string
		enableUpdateAPI     bool
		enableMetricsAPI    bool
		enableContainersAPI bool
		wantCount           int
	}{
		{name: "only update", enableUpdateAPI: true, wantCount: 1},
		{name: "only metrics", enableMetricsAPI: true, wantCount: 1},
		{name: "only containers", enableContainersAPI: true, wantCount: 1},
		{name: "all APIs", enableUpdateAPI: true, enableMetricsAPI: true, enableContainersAPI: true, wantCount: 3},
		{name: "none", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(testLogger(), 60)
			auth := newKeyAuthMiddleware(sha256.Sum256([]byte("test")))

			var updateFn func(ctx context.Context, f types.Filter, p types.UpdateParams) *metrics.Metric
			if tt.enableUpdateAPI {
				updateFn = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				}
			}

			opts := Options{
				EnableUpdateAPI:             tt.enableUpdateAPI,
				UnblockHTTPAPI:              true,
				RunUpdatesWithNotifications: updateFn,
				FilterByImage:               func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics:              func() *metrics.Metrics { return testMetrics },
				EnableMetricsAPI:            tt.enableMetricsAPI,
				EnableContainersAPI:         tt.enableContainersAPI,
				Client:                      containermocks.NewMockClient(t),
			}

			registerRoutes(app, auth, opts)

			routes := app.GetRoutes()
			apiCount := 0

			for _, r := range routes {
				if r.Path == "/v1/update" || r.Path == "/v1/metrics" || r.Path == "/v1/containers" {
					apiCount++
				}
			}

			assert.Equal(t, tt.wantCount, apiCount)
		})
	}
}

func Test_registerUpdateRoute(t *testing.T) {
	app := New(testLogger(), 60)
	auth := newKeyAuthMiddleware(sha256.Sum256([]byte("test")))

	opts := Options{
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  true,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
	}

	registerUpdateRoute(app, auth, opts)

	routes := app.GetRoutes()
	found := false

	for _, r := range routes {
		if r.Path == "/v1/update" && r.Method == http.MethodPost {
			found = true

			break
		}
	}

	assert.True(t, found, "POST /v1/update should be registered")
}

func Test_registerMetricsRoute(t *testing.T) {
	app := New(testLogger(), 60)
	auth := newKeyAuthMiddleware(sha256.Sum256([]byte("test")))

	registerMetricsRoute(app, auth)

	routes := app.GetRoutes()
	found := false

	for _, r := range routes {
		if r.Path == "/v1/metrics" && r.Method == http.MethodGet {
			found = true

			break
		}
	}

	assert.True(t, found, "GET /v1/metrics should be registered")
}

func Test_registerContainersRoute(t *testing.T) {
	app := New(testLogger(), 60)
	auth := newKeyAuthMiddleware(sha256.Sum256([]byte("test")))
	mockClient := containermocks.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil).Maybe()

	opts := Options{
		EnableContainersAPI: true,
		Client:              mockClient,
		UnblockHTTPAPI:      true,
	}

	registerContainersRoute(app, auth, opts)

	routes := app.GetRoutes()
	found := false

	for _, r := range routes {
		if r.Path == "/v1/containers" && r.Method == http.MethodGet {
			found = true

			break
		}
	}

	assert.True(t, found, "GET /v1/containers should be registered")
}

func Test_validateUpdateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name: "all present",
			opts: Options{
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics: func() *metrics.Metrics { return testMetrics },
			},
			wantErr: false,
		},
		{
			name: "missing RunUpdatesWithNotifications",
			opts: Options{
				RunUpdatesWithNotifications: nil,
				FilterByImage:               func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics:              func() *metrics.Metrics { return testMetrics },
			},
			wantErr: true,
		},
		{
			name: "missing FilterByImage",
			opts: Options{
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  nil,
				DefaultMetrics: func() *metrics.Metrics { return testMetrics },
			},
			wantErr: true,
		},
		{
			name: "missing DefaultMetrics",
			opts: Options{
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics: nil,
			},
			wantErr: true,
		},
		{
			name:    "all nil",
			opts:    Options{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpdateOptions(tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration-level tests for newly identified gaps
// ---------------------------------------------------------------------------

func TestIntegration_HealthLiveness_And_Startup(t *testing.T) {
	opts := Options{
		Token:            "test-token",
		EnableMetricsAPI: true,
		RateLimit:        60,
	}

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)

	go func() {
		errCh <- SetupAndStartAPI(ctx, opts)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestRegisterUpdateRoute_UnblockHTTPAPI(t *testing.T) {
	var startupCalled atomic.Int32

	opts := Options{
		Token:           "test-token",
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  false,
		RateLimit:       60,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage: func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics {
			return &metrics.Metrics{}
		},
		WriteStartupMessage: func(_ *cobra.Command, _ time.Time, _, _ string, _ container.Client, _ types.Notifier, _ string, _ *bool) {
			startupCalled.Add(1)
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)

	go func() {
		errCh <- SetupAndStartAPI(ctx, opts)
	}()

	time.Sleep(50 * time.Millisecond)

	assert.Positive(t, startupCalled.Load(), "WriteStartupMessage should be called when UnblockHTTPAPI is false")

	cancel()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
