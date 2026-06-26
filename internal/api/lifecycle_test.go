package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

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

func runServerAndShutdown(t *testing.T, opts config.Options) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- SetupAndStartAPI(ctx, opts)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled),
			"unexpected error: %v", err)
	case <-time.After(ShutdownGracePeriod + 2*time.Second):
		t.Fatal("server did not shut down within expected time")
	}
}

func TestSetupAndStartAPI(t *testing.T) {
	testMetrics := metrics.Default()

	tests := []struct {
		name    string
		opts    config.Options
		wantErr bool
		errMsg  string
	}{
		{
			name: "no APIs enabled",
			opts: config.Options{
				Token: "test-token",
			},
			wantErr: false,
		},
		{
			name: "nil RunUpdatesWithNotifications",
			opts: config.Options{
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
			opts: config.Options{
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
			opts: config.Options{
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

func TestSetupAndStartAPI_FullAPILifecycle(t *testing.T) {
	testMetrics := metrics.Default()

	opts := config.Options{
		Token:         "test-token",
		EventsToken:   "events-token",
		EnableFullAPI: true,
		RateLimit:     60,
		Client:        makeListContainersMock(t),
		Filter:        makeFilter(t),
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		UnblockHTTPAPI: true,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- SetupAndStartAPI(ctx, opts)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled),
			"unexpected error: %v", err)
	case <-time.After(ShutdownGracePeriod + 2*time.Second):
		t.Fatal("server did not shut down within expected time")
	}
}

func TestSetupAndStartAPI_NoAPIs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := SetupAndStartAPI(ctx, config.Options{Token: "test"})
	assert.NoError(t, err)
}

func TestSetupAndStartAPI_MetricsOnly(t *testing.T) {
	testMetrics := metrics.Default()

	opts := config.Options{
		Token:            "test-token",
		EnableMetricsAPI: true,
		RateLimit:        60,
		DefaultMetrics:   func() *metrics.Metrics { return testMetrics },
	}

	runServerAndShutdown(t, opts)
}

func TestSetupAndStartAPI_ContainersOnly(t *testing.T) {
	opts := config.Options{
		Token:               "test-token",
		EnableContainersAPI: true,
		RateLimit:           60,
		Client:              makeListContainersMock(t),
		Filter:              makeFilter(t),
	}

	runServerAndShutdown(t, opts)
}

func TestSetupAndStartAPI_CheckOnly(t *testing.T) {
	testMetrics := metrics.Default()

	opts := config.Options{
		Token:          "test-token",
		EnableCheckAPI: true,
		RateLimit:      60,
		Client:         makeListContainersMock(t),
		Filter:         makeFilter(t),
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
	}

	runServerAndShutdown(t, opts)
}

func TestSetupAndStartAPI_AllAPIs(t *testing.T) {
	testMetrics := metrics.Default()

	opts := config.Options{
		Token:               "test-token",
		EventsToken:         "events-token",
		EnableUpdateAPI:     true,
		EnableMetricsAPI:    true,
		EnableContainersAPI: true,
		EnableCheckAPI:      true,
		EnableHistoryAPI:    true,
		EnableImagesAPI:     true,
		EnableConfigAPI:     true,
		EnableEventsAPI:     true,
		RateLimit:           60,
		Client:              makeListContainersMock(t),
		Filter:              makeFilter(t),
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		UnblockHTTPAPI: true,
	}

	runServerAndShutdown(t, opts)
}

func TestIntegration_HealthLiveness_And_Startup(t *testing.T) {
	testMetrics := metrics.Default()

	opts := config.Options{
		Token:            "test-token",
		EnableMetricsAPI: true,
		RateLimit:        60,
		DefaultMetrics:   func() *metrics.Metrics { return testMetrics },
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

	opts := config.Options{
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
	case err := <-errCh:
		assert.True(t, err == nil || errors.Is(err, context.Canceled),
			"unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
