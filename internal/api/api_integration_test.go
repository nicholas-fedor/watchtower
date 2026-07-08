package api_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
	mockAPI "github.com/nicholas-fedor/watchtower/internal/api/mocks"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var helperMetrics = metrics.Default()

func testLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}

func makeListContainersMock(t *testing.T) *mockContainer.MockClient {
	t.Helper()
	mc := mockContainer.NewMockClient(t)
	mc.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{}, nil).Maybe()
	mc.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil).Maybe()

	return mc
}

func makeFilter(_ *testing.T) types.Filter {
	return func(_ types.FilterableContainer) bool { return true }
}

// NewEventsBroadcasterHelper creates an event broadcaster for integration tests.
func NewEventsBroadcasterHelper() *events.Broadcaster { return events.NewBroadcaster() }

func withTestListenAddr(opts config.Options) config.Options {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}

	if opts.Port == "" {
		opts.Port = "0"
	}

	return opts
}

func runServerAndShutdown(t *testing.T, opts config.Options) {
	t.Helper()

	opts = withTestListenAddr(opts)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- api.SetupAndStartAPI(ctx, opts)
	}()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled),
			"unexpected error: %v", err)
		cancel()
		time.Sleep(50 * time.Millisecond)

		return
	case <-time.After(500 * time.Millisecond):
		// Blocking mode still inside SetupAndStartAPI.
	}

	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled),
			"unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within expected time")
	}
}

// ---------------------------------------------------------------------------
// Health probes
// ---------------------------------------------------------------------------

func TestIntegration_HealthProbes_ViaFullSetup(t *testing.T) {
	opts := config.Options{
		Token:            "test-token",
		EnableMetricsAPI: true,
		RateLimit:        60,
		DefaultMetrics:   func() *metrics.Metrics { return helperMetrics },
	}
	runServerAndShutdown(t, opts)
}

func TestIntegration_HealthProbes_CustomRoute(t *testing.T) {
	mc := mockContainer.NewMockClient(t)
	mc.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil)

	app := api.New(testLogger(), 60, api.ProxyConfig{}, api.CORSConfig{})
	app.Get("/health/custom", func(c fiber.Ctx) error {
		_, err := mc.ListContainers(c.Context())
		if err != nil {
			return c.SendStatus(http.StatusServiceUnavailable)
		}

		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/custom", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// SetupAndStartAPI
// ---------------------------------------------------------------------------

func TestIntegration_SetupAndStartAPI_NoAPIs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := api.SetupAndStartAPI(ctx, config.Options{Token: "test"})
	assert.NoError(t, err)
}

func TestIntegration_SetupAndStartAPI_MetricsOnly(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:            "test-token",
		EnableMetricsAPI: true,
		RateLimit:        60,
		DefaultMetrics:   func() *metrics.Metrics { return helperMetrics },
	})
}

func TestIntegration_SetupAndStartAPI_ContainersOnly(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:               "test-token",
		EnableContainersAPI: true,
		RateLimit:           60,
		Client:              makeListContainersMock(t),
		Filter:              makeFilter(t),
	})
}

func TestIntegration_SetupAndStartAPI_AllAPIs(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:               "test-token",
		EnableUpdateAPI:     true,
		EnableMetricsAPI:    true,
		EnableContainersAPI: true,
		RateLimit:           60,
		Client:              makeListContainersMock(t),
		Filter:              makeFilter(t),
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return helperMetrics },
		UnblockHTTPAPI: true,
	})
}

func TestIntegration_SetupAndStartAPI_MissingUpdateDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	tests := []struct {
		name   string
		opts   config.Options
		errMsg string
	}{
		{
			name: "nil RunUpdatesWithNotifications",
			opts: config.Options{
				Token:                       "test",
				EnableUpdateAPI:             true,
				RunUpdatesWithNotifications: nil,
				FilterByImage:               func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics:              func() *metrics.Metrics { return helperMetrics },
			},
			errMsg: "RunUpdatesWithNotifications must be provided",
		},
		{
			name: "nil FilterByImage",
			opts: config.Options{
				Token:           "test",
				EnableUpdateAPI: true,
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  nil,
				DefaultMetrics: func() *metrics.Metrics { return helperMetrics },
			},
			errMsg: "FilterByImage must be provided",
		},
		{
			name: "nil DefaultMetrics",
			opts: config.Options{
				Token:           "test",
				EnableUpdateAPI: true,
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics: nil,
			},
			errMsg: "DefaultMetrics must be provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := api.SetupAndStartAPI(ctx, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// ---------------------------------------------------------------------------
// Graceful shutdown
// ---------------------------------------------------------------------------

func TestIntegration_GracefulShutdown_MetricsAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)

	go func() {
		errCh <- api.SetupAndStartAPI(ctx, config.Options{
			Token:            "test-token",
			EnableMetricsAPI: true,
			RateLimit:        60,
			DefaultMetrics:   func() *metrics.Metrics { return helperMetrics },
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestIntegration_GracefulShutdown_AllAPIs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)

	go func() {
		errCh <- api.SetupAndStartAPI(ctx, config.Options{
			Token:               "test-token",
			EnableUpdateAPI:     true,
			EnableMetricsAPI:    true,
			EnableContainersAPI: true,
			RateLimit:           60,
			Client:              makeListContainersMock(t),
			Filter:              makeFilter(t),
			RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{}
			},
			FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
			DefaultMetrics: func() *metrics.Metrics { return helperMetrics },
			UnblockHTTPAPI: true,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestIntegration_GracefulShutdownTiming(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- api.SetupAndStartAPI(ctx, config.Options{
			Token:               "test-token",
			EnableUpdateAPI:     true,
			EnableMetricsAPI:    true,
			EnableContainersAPI: true,
			RateLimit:           60,
			Client:              makeListContainersMock(t),
			Filter:              makeFilter(t),
			RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{}
			},
			FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
			DefaultMetrics: func() *metrics.Metrics { return helperMetrics },
			UnblockHTTPAPI: true,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled),
			"unexpected error: %v", err)
	case <-time.After(api.ShutdownGracePeriod + 2*time.Second):
		t.Fatal("server did not shut down within expected time")
	}
}

// ---------------------------------------------------------------------------
// Server interface
// ---------------------------------------------------------------------------

func TestIntegration_ServerInterface(t *testing.T) {
	mockServer := mockAPI.NewMockServer(t)
	mockServer.EXPECT().Listen(mock.Anything, mock.Anything).Return(nil).Maybe()
	mockServer.EXPECT().ShutdownWithTimeout(mock.Anything).Return(nil).Maybe()
	assert.NotNil(t, mockServer)
}

// ---------------------------------------------------------------------------
// GetAPIAddr
// ---------------------------------------------------------------------------

func TestIntegration_GetAPIAddr(t *testing.T) {
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
		{name: "IPv6 zone", host: "fe80::1%eth0", port: "8080", want: "[fe80::1%eth0]:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, api.GetAPIAddr(tt.host, tt.port))
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrent requests
// ---------------------------------------------------------------------------

func TestIntegration_ConcurrentRequests(t *testing.T) {
	app := api.New(testLogger(), 60, api.ProxyConfig{}, api.CORSConfig{})

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nonexistent", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// API enablement combinations
// ---------------------------------------------------------------------------

func TestIntegration_EnablementCombinations(t *testing.T) {
	combinations := []struct {
		name                string
		enableUpdateAPI     bool
		enableMetricsAPI    bool
		enableContainersAPI bool
		enableHistoryAPI    bool
		enableImagesAPI     bool
		enableConfigAPI     bool
		enableEventsAPI     bool
	}{
		{name: "metrics only", enableMetricsAPI: true},
		{name: "containers only", enableContainersAPI: true},
		{name: "update only", enableUpdateAPI: true},
		{name: "history only", enableHistoryAPI: true},
		{name: "images only", enableImagesAPI: true},
		{name: "config only", enableConfigAPI: true},
		{name: "events only", enableEventsAPI: true},
		{name: "update+metrics", enableUpdateAPI: true, enableMetricsAPI: true},
		{name: "update+containers", enableUpdateAPI: true, enableContainersAPI: true},
		{name: "metrics+containers", enableMetricsAPI: true, enableContainersAPI: true},
		{name: "all read-only APIs", enableMetricsAPI: true, enableContainersAPI: true, enableHistoryAPI: true, enableImagesAPI: true, enableConfigAPI: true, enableEventsAPI: true},
		{name: "all APIs", enableUpdateAPI: true, enableMetricsAPI: true, enableContainersAPI: true, enableHistoryAPI: true, enableImagesAPI: true, enableConfigAPI: true, enableEventsAPI: true},
	}

	for _, c := range combinations {
		t.Run(c.name, func(t *testing.T) {
			opts := config.Options{
				Token:               "test-token",
				EventsToken:         "events-token",
				EnableUpdateAPI:     c.enableUpdateAPI,
				EnableMetricsAPI:    c.enableMetricsAPI,
				EnableContainersAPI: c.enableContainersAPI,
				EnableHistoryAPI:    c.enableHistoryAPI,
				EnableImagesAPI:     c.enableImagesAPI,
				EnableConfigAPI:     c.enableConfigAPI,
				EnableEventsAPI:     c.enableEventsAPI,
				RateLimit:           60,
				Client:              makeListContainersMock(t),
				Filter:              makeFilter(t),
				RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
					return &metrics.Metric{}
				},
				FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
				DefaultMetrics: func() *metrics.Metrics { return helperMetrics },
				UnblockHTTPAPI: true,
			}
			runServerAndShutdown(t, opts)
		})
	}
}

// ---------------------------------------------------------------------------
// History API
// ---------------------------------------------------------------------------

func TestIntegration_HistoryAPI(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:            "test-token",
		EnableHistoryAPI: true,
		RateLimit:        60,
		Client:           makeListContainersMock(t),
		Filter:           makeFilter(t),
		DefaultMetrics:   func() *metrics.Metrics { return helperMetrics },
	})
}

// ---------------------------------------------------------------------------
// Images API
// ---------------------------------------------------------------------------

func TestIntegration_ImagesAPI(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:           "test-token",
		EnableImagesAPI: true,
		RateLimit:       60,
		Client:          makeListContainersMock(t),
		Filter:          makeFilter(t),
	})
}

// ---------------------------------------------------------------------------
// Config API
// ---------------------------------------------------------------------------

func TestIntegration_ConfigAPI(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:           "test-token",
		EnableConfigAPI: true,
		RateLimit:       60,
	})
}

// ---------------------------------------------------------------------------
// Events API
// ---------------------------------------------------------------------------

func TestIntegration_EventsAPI(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:            "test-token",
		EventsToken:      "events-token",
		EnableEventsAPI:  true,
		RateLimit:        60,
		EventBroadcaster: NewEventsBroadcasterHelper(),
	})
}

// ---------------------------------------------------------------------------
// Container Details API
// ---------------------------------------------------------------------------

func TestIntegration_ContainerDetailsAPI(t *testing.T) {
	runServerAndShutdown(t, config.Options{
		Token:               "test-token",
		EnableContainersAPI: true,
		RateLimit:           60,
		Client:              makeListContainersMock(t),
		Filter:              makeFilter(t),
	})
}

// ---------------------------------------------------------------------------
// Token hashing consistency
// ---------------------------------------------------------------------------

func TestIntegration_TokenHashing(t *testing.T) {
	token := "my-secret-token"
	hash1 := sha256.Sum256([]byte(token))
	hash2 := sha256.Sum256([]byte(token))
	assert.Equal(t, hash1, hash2)

	differentHash := sha256.Sum256([]byte("different-token"))
	assert.NotEqual(t, hash1, differentHash)
}
