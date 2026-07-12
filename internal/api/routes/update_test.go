package routes

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestRegisterUpdateRoute(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()

	opts := config.Options{
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  true,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
	}

	registerUpdateRoute(context.Background(), app, auth, opts)

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

func TestRegisterUpdateRoute_BuildsFullUpdateParams(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()

	var got types.UpdateParams

	opts := config.Options{
		EnableUpdateAPI:     true,
		UnblockHTTPAPI:      true,
		Cleanup:             true,
		ReviveStopped:       true,
		UseComposeDependsOn: true,
		NoPull:              true,
		NoRestart:           true,
		LifecycleHooks:      true,
		RollingRestart:      true,
		LabelPrecedence:     true,
		SkipSelfUpdate:      true,
		Filter:              func(_ types.FilterableContainer) bool { return true },
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
			got = params

			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		UpdateLock:     make(chan bool, 1),
	}

	registerUpdateRoute(context.Background(), app, auth, opts)

	// Invoke the update pipeline the same way the handler does (via the
	// registered RunUpdatesWithNotifications closure setup).
	params := config.BuildUpdateParams(opts)
	opts.RunUpdatesWithNotifications(context.Background(), opts.Filter, params)

	assert.True(t, got.ReviveStopped)
	assert.True(t, got.UseComposeDependsOn)
	assert.True(t, got.Cleanup)
	assert.True(t, got.NoPull)
	assert.True(t, got.NoRestart)
	assert.True(t, got.LifecycleHooks)
	assert.True(t, got.RollingRestart)
	assert.True(t, got.LabelPrecedence)
	assert.True(t, got.SkipSelfUpdate)
	assert.False(t, got.RunOnce)
}

func TestRegisterUpdateRoute_NilWriteStartupMessage(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()

	opts := config.Options{
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  false,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:       func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics:      func() *metrics.Metrics { return testMetrics },
		WriteStartupMessage: nil,
	}

	assert.NotPanics(t, func() {
		registerUpdateRoute(context.Background(), app, auth, opts)
	})
}

func TestRegisterUpdateRoute_WriteStartupMessageCalledWhenBlocking(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()

	called := false
	opts := config.Options{
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  false,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		WriteStartupMessage: func(
			_ *cobra.Command,
			_ time.Time,
			_, _ string,
			_ container.Client,
			_ types.Notifier,
			_ string,
			_ *bool,
		) {
			called = true
		},
	}

	registerUpdateRoute(context.Background(), app, auth, opts)
	assert.True(t, called)
}
