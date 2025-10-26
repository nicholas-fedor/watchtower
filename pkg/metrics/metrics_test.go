// Package metrics provides functionality for tracking and exposing Watchtower scan metrics.
// It integrates with Prometheus to monitor container scan outcomes, including scanned, updated, and failed counts.
package metrics

import (
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestNewMetric(t *testing.T) {
	type args struct {
		report types.Report
	}

	tests := []struct {
		name string
		args args
		want *Metric
	}{
		{
			name: "empty report",
			args: args{
				report: func() types.Report {
					mock := mocks.NewMockReport(t)
					mock.EXPECT().Scanned().Return([]types.ContainerReport{})
					mock.EXPECT().Updated().Return([]types.ContainerReport{})
					mock.EXPECT().Failed().Return([]types.ContainerReport{})

					return mock
				}(),
			},
			want: &Metric{
				Scanned: 0,
				Updated: 0,
				Failed:  0,
			},
		},
		{
			name: "mixed report",
			args: args{
				report: func() types.Report {
					mock := mocks.NewMockReport(t)
					mock.EXPECT().Scanned().Return(make([]types.ContainerReport, 3))
					mock.EXPECT().Updated().Return(make([]types.ContainerReport, 1))
					mock.EXPECT().Failed().Return(make([]types.ContainerReport, 1))

					return mock
				}(),
			},
			want: &Metric{
				Scanned: 3,
				Updated: 1, // Only count actually updated containers
				Failed:  1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewMetric(tt.args.report); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewMetric() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetrics_QueueIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		m    *Metrics
		want bool
	}{
		{
			name: "empty queue",
			m:    &Metrics{channel: make(chan *Metric, 10)},
			want: true,
		},
		{
			name: "non-empty queue",
			m: func() *Metrics {
				ch := make(chan *Metric, 10)
				ch <- &Metric{Scanned: 1}

				return &Metrics{channel: ch}
			}(),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.QueueIsEmpty(); got != tt.want {
				t.Errorf("Metrics.QueueIsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetrics_Register(t *testing.T) {
	type args struct {
		metric *Metric
	}

	tests := []struct {
		name string
		m    *Metrics
		args args
	}{
		{
			name: "register metric",
			m: func() *Metrics {
				metrics = &Metrics{
					channel: make(chan *Metric, 10),
				} // Set global metrics for Register

				return metrics
			}(),
			args: args{
				metric: &Metric{Scanned: 1, Updated: 2, Failed: 0},
			},
		},
		{
			name: "register nil metric",
			m: func() *Metrics {
				metrics = &Metrics{channel: make(chan *Metric, 10)} // Reset global metrics

				return metrics
			}(),
			args: args{
				metric: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.Register(tt.args.metric)

			select {
			case got := <-tt.m.channel:
				if !reflect.DeepEqual(got, tt.args.metric) {
					t.Errorf("Metrics.Register() enqueued %v, want %v", got, tt.args.metric)
				}
			default:
				t.Errorf("Metrics.Register() did not enqueue metric")
			}
		})
	}
}

func TestDefault(t *testing.T) {
	// Reset metrics to nil to force initialization, but only if not already tested
	originalMetrics := metrics
	metrics = nil

	defer func() { metrics = originalMetrics }() // Restore original state after test

	got := Default()

	tests := []struct {
		name string
	}{
		{name: "new metrics instance"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Default() returned: %+v", got)

			if got == nil {
				t.Fatalf("Default() returned nil pointer")
			}

			if got.channel == nil {
				t.Errorf("Default().channel is nil")
			} else if cap(got.channel) != 10 {
				t.Errorf("Default() channel capacity = %d, want 10", cap(got.channel))
			}

			if got.scanned == nil {
				t.Errorf("Default().scanned is nil")
			}

			if got.updated == nil {
				t.Errorf("Default().updated is nil")
			}

			if got.failed == nil {
				t.Errorf("Default().failed is nil")
			}

			if got.total == nil {
				t.Errorf("Default().total is nil")
			}

			if got.skipped == nil {
				t.Errorf("Default().skipped is nil")
			}

			gotAgain := Default()
			if got != gotAgain {
				t.Errorf("Default() did not return singleton: got %p, gotAgain %p", got, gotAgain)
			}
		})
	}
}

func TestNewWithRegistry(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "new metrics with registry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()

			got, err := NewWithRegistry(registry)
			if err != nil {
				t.Fatalf("NewWithRegistry() returned error: %v", err)
			}

			defer func() { close(got.channel) }()

			if got == nil {
				t.Fatalf("NewWithRegistry() returned nil pointer")
			}

			if got.channel == nil {
				t.Errorf("NewWithRegistry().channel is nil")
			} else if cap(got.channel) != 10 {
				t.Errorf("NewWithRegistry() channel capacity = %d, want 10", cap(got.channel))
			}

			if got.scanned == nil {
				t.Errorf("NewWithRegistry().scanned is nil")
			}

			if got.updated == nil {
				t.Errorf("NewWithRegistry().updated is nil")
			}

			if got.failed == nil {
				t.Errorf("NewWithRegistry().failed is nil")
			}

			if got.total == nil {
				t.Errorf("NewWithRegistry().total is nil")
			}

			if got.skipped == nil {
				t.Errorf("NewWithRegistry().skipped is nil")
			}

			// Gather metrics from the registry and verify registration
			metricFamilies, err := registry.Gather()
			if err != nil {
				t.Fatalf("Failed to gather metrics: %v", err)
			}

			if len(metricFamilies) != 5 {
				t.Errorf("Expected 5 metric families registered, got %d", len(metricFamilies))
			}

			expectedNames := map[string]bool{
				"watchtower_containers_scanned":  true,
				"watchtower_containers_updated":  true,
				"watchtower_containers_failed":   true,
				"watchtower_scans_total":         true,
				"watchtower_scans_skipped_total": true,
			}

			for _, mf := range metricFamilies {
				if !expectedNames[*mf.Name] {
					t.Errorf("Unexpected metric family registered: %s", *mf.Name)
				}
			}
		})
	}
}

func TestRegisterScan(t *testing.T) {
	type args struct {
		metric *Metric
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "register scan metric",
			args: args{
				metric: &Metric{Scanned: 2, Updated: 1, Failed: 0},
			},
		},
		{
			name: "register nil metric",
			args: args{
				metric: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset metrics and set up a fresh instance
			metrics = &Metrics{channel: make(chan *Metric, 10)}
			metrics.RegisterScan(tt.args.metric)

			select {
			case got := <-metrics.channel:
				if !reflect.DeepEqual(got, tt.args.metric) {
					t.Errorf("RegisterScan() enqueued %v, want %v", got, tt.args.metric)
				}
			default:
				t.Errorf("RegisterScan() did not enqueue metric")
			}
		})
	}
}

func TestMetrics_HandleUpdate(t *testing.T) {
	tests := []struct {
		name string
		m    *Metrics
	}{
		{
			name: "handle valid metric",
			m: &Metrics{
				channel: make(chan *Metric, 1),
				stopCh:  make(chan struct{}),
				scanned: promauto.NewGauge(prometheus.GaugeOpts{Name: "test_scanned"}),
				updated: promauto.NewGauge(prometheus.GaugeOpts{Name: "test_updated"}),
				failed:  promauto.NewGauge(prometheus.GaugeOpts{Name: "test_failed"}),
				total:   promauto.NewCounter(prometheus.CounterOpts{Name: "test_total"}),
				skipped: promauto.NewCounter(prometheus.CounterOpts{Name: "test_skipped"}),
			},
		},
		{
			name: "handle nil metric (skipped)",
			m: &Metrics{
				channel: make(chan *Metric, 1),
				stopCh:  make(chan struct{}),
				scanned: promauto.NewGauge(prometheus.GaugeOpts{Name: "test_scanned_skip"}),
				updated: promauto.NewGauge(prometheus.GaugeOpts{Name: "test_updated_skip"}),
				failed:  promauto.NewGauge(prometheus.GaugeOpts{Name: "test_failed_skip"}),
				total:   promauto.NewCounter(prometheus.CounterOpts{Name: "test_total_skip"}),
				skipped: promauto.NewCounter(prometheus.CounterOpts{Name: "test_skipped_skip"}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run HandleUpdate and deterministically wait for completion
			done := make(chan struct{})

			go func() {
				tt.m.HandleUpdate()
				close(done)
			}()

			// Send metric to channel
			if tt.name == "handle valid metric" {
				tt.m.channel <- &Metric{Scanned: 3, Updated: 2, Failed: 1}
			} else {
				tt.m.channel <- nil
			}

			// Close stopCh to signal shutdown
			close(tt.m.stopCh)

			select {
			case <-done:
				// processed to completion
			case <-time.After(1 * time.Second):
				t.Fatal("HandleUpdate timed out")
			}
		})
	}
}
