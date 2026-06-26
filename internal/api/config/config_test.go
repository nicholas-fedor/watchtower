package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestValidateUpdateOptions(t *testing.T) {
	testMetrics := metrics.Default()

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
			err := ValidateUpdateOptions(tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestErrSentinelValues(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{name: "ErrMissingRunUpdatesWithNotifications", err: ErrMissingRunUpdatesWithNotifications, msg: "RunUpdatesWithNotifications must be provided"},
		{name: "ErrMissingFilterByImage", err: ErrMissingFilterByImage, msg: "FilterByImage must be provided"},
		{name: "ErrMissingDefaultMetrics", err: ErrMissingDefaultMetrics, msg: "DefaultMetrics must be provided"},
		{name: "ErrMissingAPIToken", err: ErrMissingAPIToken, msg: "API token is empty or unset"},
		{name: "ErrMissingEventsAPIToken", err: ErrMissingEventsAPIToken, msg: "events API token is required"},
		{name: "ErrMissingTLSConfig", err: ErrMissingTLSConfig, msg: "TLS requires both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.err.Error(), tt.msg)
		})
	}
}
