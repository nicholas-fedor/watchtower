package routes

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestValidateUpdateOptions verifies that each update API dependency is
// required. Every case supplies all the others, so each one fails only on the
// dependency it leaves out, whatever order the checks run in.
func TestValidateUpdateOptions(t *testing.T) {
	testMetrics := metrics.Default()

	runUpdates := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
		return &metrics.Metric{}
	}
	filterByImage := func(_ []string, f types.Filter) types.Filter { return f }
	defaultMetrics := func() *metrics.Metrics { return testMetrics }

	tests := []struct {
		name    string
		opts    Options
		wantErr error
	}{
		{
			name: "all present",
			opts: Options{
				RunUpdatesWithNotifications: runUpdates,
				FilterByImage:               filterByImage,
				DefaultMetrics:              defaultMetrics,
				UpdateLock:                  scheduling.NewUpdateLock(),
			},
			wantErr: nil,
		},
		{
			name: "missing RunUpdatesWithNotifications",
			opts: Options{
				RunUpdatesWithNotifications: nil,
				FilterByImage:               filterByImage,
				DefaultMetrics:              defaultMetrics,
				UpdateLock:                  scheduling.NewUpdateLock(),
			},
			wantErr: errMissingRunUpdatesWithNotifications,
		},
		{
			name: "missing FilterByImage",
			opts: Options{
				RunUpdatesWithNotifications: runUpdates,
				FilterByImage:               nil,
				DefaultMetrics:              defaultMetrics,
				UpdateLock:                  scheduling.NewUpdateLock(),
			},
			wantErr: ErrMissingFilterByImage,
		},
		{
			name: "missing DefaultMetrics",
			opts: Options{
				RunUpdatesWithNotifications: runUpdates,
				FilterByImage:               filterByImage,
				DefaultMetrics:              nil,
				UpdateLock:                  scheduling.NewUpdateLock(),
			},
			wantErr: ErrMissingDefaultMetrics,
		},
		{
			name: "missing UpdateLock",
			opts: Options{
				RunUpdatesWithNotifications: runUpdates,
				FilterByImage:               filterByImage,
				DefaultMetrics:              defaultMetrics,
				UpdateLock:                  nil,
			},
			wantErr: errMissingUpdateLock,
		},
		{
			name:    "all nil",
			opts:    Options{},
			wantErr: errMissingRunUpdatesWithNotifications,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpdateOptions(tt.opts)
			if tt.wantErr == nil {
				assert.NoError(t, err)

				return
			}

			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestBuildUpdateParams(t *testing.T) {
	t.Parallel()

	opts := Options{
		BaseParams: types.UpdateParams{
			Cleanup:             true,
			NoRestart:           true,
			ReviveStopped:       true,
			MonitorOnly:         true,
			NoPull:              true,
			LifecycleHooks:      true,
			RollingRestart:      true,
			LabelPrecedence:     true,
			UseComposeDependsOn: true,
			SkipSelfUpdate:      true,
			CooldownDelay:       24 * time.Hour,
			RunOnce:             true, // must be cleared for HTTP path
		},
	}

	params := BuildUpdateParams(opts)

	assert.True(t, params.Cleanup)
	assert.True(t, params.NoRestart)
	assert.True(t, params.ReviveStopped)
	assert.True(t, params.MonitorOnly)
	assert.True(t, params.NoPull)
	assert.True(t, params.LifecycleHooks)
	assert.True(t, params.RollingRestart)
	assert.True(t, params.LabelPrecedence)
	assert.True(t, params.UseComposeDependsOn)
	assert.True(t, params.SkipSelfUpdate)
	assert.Equal(t, 24*time.Hour, params.CooldownDelay)
	assert.False(t, params.RunOnce, "HTTP updates are never run-once")
}

func TestErrSentinelValues(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{name: "errMissingRunUpdatesWithNotifications", err: errMissingRunUpdatesWithNotifications, msg: "RunUpdatesWithNotifications must be provided"},
		{name: "ErrMissingFilterByImage", err: ErrMissingFilterByImage, msg: "FilterByImage must be provided"},
		{name: "ErrMissingDefaultMetrics", err: ErrMissingDefaultMetrics, msg: "DefaultMetrics must be provided"},
		{name: "errMissingUpdateLock", err: errMissingUpdateLock, msg: "UpdateLock must be provided"},
		{name: "ErrMissingAPIToken", err: ErrMissingAPIToken, msg: "API token is empty or unset"},
		{name: "ErrMissingEventsAPIToken", err: ErrMissingEventsAPIToken, msg: "events API token is required"},
		{name: "ErrMissingEventBroadcaster", err: ErrMissingEventBroadcaster, msg: "EventBroadcaster must be provided"},
		{name: "ErrMissingTLSConfig", err: ErrMissingTLSConfig, msg: "TLS requires both"},
		{name: "ErrMissingLogger", err: ErrMissingLogger, msg: "API Logger must be provided"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.err.Error(), tt.msg)
		})
	}
}
