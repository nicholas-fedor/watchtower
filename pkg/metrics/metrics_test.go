// Package metrics provides functionality for tracking and exposing Watchtower scan metrics.
// It integrates with Prometheus to monitor container scan outcomes, including scanned, updated, and failed counts.
package metrics

import (
	"reflect"
	"sync"
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
					mock.EXPECT().Restarted().Return([]types.ContainerReport{})

					return mock
				}(),
			},
			want: &Metric{
				Scanned:   0,
				Updated:   0,
				Failed:    0,
				Restarted: 0,
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
					mock.EXPECT().Restarted().Return(make([]types.ContainerReport, 2))

					return mock
				}(),
			},
			want: &Metric{
				Scanned:   3,
				Updated:   1, // Only count actually updated containers
				Failed:    1,
				Restarted: 2,
			},
		},
		{
			name: "only restarted containers",
			args: args{
				report: func() types.Report {
					mock := mocks.NewMockReport(t)
					mock.EXPECT().Scanned().Return(make([]types.ContainerReport, 5))
					mock.EXPECT().Updated().Return([]types.ContainerReport{})
					mock.EXPECT().Failed().Return([]types.ContainerReport{})
					mock.EXPECT().Restarted().Return(make([]types.ContainerReport, 5))

					return mock
				}(),
			},
			want: &Metric{
				Scanned:   5,
				Updated:   0,
				Failed:    0,
				Restarted: 5,
			},
		},
		{
			name: "restarted with failures",
			args: args{
				report: func() types.Report {
					mock := mocks.NewMockReport(t)
					mock.EXPECT().Scanned().Return(make([]types.ContainerReport, 10))
					mock.EXPECT().Updated().Return(make([]types.ContainerReport, 3))
					mock.EXPECT().Failed().Return(make([]types.ContainerReport, 2))
					mock.EXPECT().Restarted().Return(make([]types.ContainerReport, 4))

					return mock
				}(),
			},
			want: &Metric{
				Scanned:   10,
				Updated:   3,
				Failed:    2,
				Restarted: 4,
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

func TestNewMetric_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		report      types.Report
		want        *Metric
		shouldPanic bool
	}{
		{
			name: "nil report",
			report: func() types.Report {
				return nil
			}(),
			want:        nil,
			shouldPanic: true,
		},
		{
			name: "report with nil slices",
			report: func() types.Report {
				mock := mocks.NewMockReport(t)
				mock.EXPECT().Scanned().Return(nil)
				mock.EXPECT().Updated().Return(nil)
				mock.EXPECT().Failed().Return(nil)
				mock.EXPECT().Restarted().Return(nil)

				return mock
			}(),
			want: &Metric{
				Scanned:   0,
				Updated:   0,
				Failed:    0,
				Restarted: 0,
			},
			shouldPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("NewMetric() expected panic but didn't get one")
					}
				}()

				NewMetric(tt.report)
			} else {
				got := NewMetric(tt.report)
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("NewMetric() = %v, want %v", got, tt.want)
				}
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

func TestMetrics_StateTransitions(t *testing.T) {
	registry := prometheus.NewRegistry()

	m, err := NewWithRegistry(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	t.Cleanup(func() { m.Shutdown() })

	tests := []struct {
		name           string
		initialMetric  *Metric
		expectedValues map[string]float64
	}{
		{
			name: "initial state",
			initialMetric: &Metric{
				Scanned:   0,
				Updated:   0,
				Failed:    0,
				Restarted: 0,
			},
			expectedValues: map[string]float64{
				"watchtower_containers_scanned":   0,
				"watchtower_containers_updated":   0,
				"watchtower_containers_failed":    0,
				"watchtower_containers_restarted": 0,
			},
		},
		{
			name: "after first scan with restarted",
			initialMetric: &Metric{
				Scanned:   5,
				Updated:   2,
				Failed:    1,
				Restarted: 3,
			},
			expectedValues: map[string]float64{
				"watchtower_containers_scanned":   5,
				"watchtower_containers_updated":   2,
				"watchtower_containers_failed":    1,
				"watchtower_containers_restarted": 3,
			},
		},
		{
			name: "after second scan with more restarted",
			initialMetric: &Metric{
				Scanned:   8,
				Updated:   3,
				Failed:    0,
				Restarted: 5,
			},
			expectedValues: map[string]float64{
				"watchtower_containers_scanned":   8,
				"watchtower_containers_updated":   3,
				"watchtower_containers_failed":    0,
				"watchtower_containers_restarted": 5,
			},
		},
	}

	done := make(chan struct{})

	go func() {
		m.HandleUpdate()
		close(done)
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.Register(tt.initialMetric)

			// Give some time for the metric to be processed
			time.Sleep(10 * time.Millisecond)

			// Gather metrics and verify
			metricFamilies, err := registry.Gather()
			if err != nil {
				t.Fatalf("Failed to gather metrics: %v", err)
			}

			for _, mf := range metricFamilies {
				if expectedValue, exists := tt.expectedValues[*mf.Name]; exists {
					if len(mf.Metric) == 0 {
						t.Errorf("No metrics found for %s", *mf.Name)

						continue
					}

					actualValue := *mf.Metric[0].Gauge.Value
					if actualValue != expectedValue {
						t.Errorf("Metric %s = %v, want %v", *mf.Name, actualValue, expectedValue)
					}
				}
			}
		})
	}

	// Shutdown the handler
	m.Shutdown()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("HandleUpdate did not shutdown within timeout")
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
				metric: &Metric{Scanned: 1, Updated: 2, Failed: 0, Restarted: 0},
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

func TestMetrics_PriorityOrdering(t *testing.T) {
	registry := prometheus.NewRegistry()

	m, err := NewWithRegistry(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	t.Cleanup(func() { m.Shutdown() })

	// Test that restarted metrics are aggregated correctly with other metrics
	metrics := []*Metric{
		{Scanned: 2, Updated: 1, Failed: 0, Restarted: 1},
		{Scanned: 3, Updated: 0, Failed: 1, Restarted: 2},
		{Scanned: 1, Updated: 1, Failed: 0, Restarted: 0},
	}

	done := make(chan struct{})

	go func() {
		m.HandleUpdate()
		close(done)
	}()

	// Register metrics in sequence
	for _, metric := range metrics {
		m.Register(metric)
		time.Sleep(5 * time.Millisecond) // Small delay to ensure ordering
	}

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	// Gather final metrics
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify aggregated values
	expectedTotals := map[string]float64{
		"watchtower_containers_scanned":   1, // Last metric value
		"watchtower_containers_updated":   1, // Last metric value
		"watchtower_containers_failed":    0, // Last metric value
		"watchtower_containers_restarted": 0, // Last metric value
	}

	for _, mf := range metricFamilies {
		if expectedValue, exists := expectedTotals[*mf.Name]; exists {
			if len(mf.Metric) == 0 {
				t.Errorf("No metrics found for %s", *mf.Name)

				continue
			}

			actualValue := *mf.Metric[0].Gauge.Value
			if actualValue != expectedValue {
				t.Errorf(
					"Final aggregated metric %s = %v, want %v",
					*mf.Name,
					actualValue,
					expectedValue,
				)
			}
		}
	}

	// Shutdown
	m.Shutdown()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("HandleUpdate did not shutdown within timeout")
	}
}

func TestDefault(t *testing.T) {
	// Reset metrics to nil to force initialization, but only if not already tested
	originalMetrics := metrics
	metrics = nil

	defer func() { metrics = originalMetrics }() // Restore original state after test

	got := Default()

	t.Cleanup(func() { got.Shutdown() })

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

			if got.restarted == nil {
				t.Errorf("Default().restarted is nil")
			}

			if got.total == nil {
				t.Errorf("Default().total is nil")
			}

			if got.skipped == nil {
				t.Errorf("Default().skipped is nil")
			}

			if got.dropped == nil {
				t.Errorf("Default().dropped is nil")
			}

			gotAgain := Default()
			if got != gotAgain {
				t.Errorf("Default() did not return singleton: got %p, gotAgain %p", got, gotAgain)
			}
		})
	}
}

func TestMetrics_IntegrationWithOtherTypes(t *testing.T) {
	registry := prometheus.NewRegistry()

	m, err := NewWithRegistry(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	t.Cleanup(func() { m.Shutdown() })

	// Test comprehensive integration of all metric types including restarted
	testMetric := &Metric{
		Scanned:   10,
		Updated:   4,
		Failed:    2,
		Restarted: 3,
	}

	done := make(chan struct{})

	go func() {
		m.HandleUpdate()
		close(done)
	}()

	// Register the comprehensive metric
	m.Register(testMetric)
	time.Sleep(10 * time.Millisecond)

	// Gather and verify all metrics are set correctly
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	expectedValues := map[string]float64{
		"watchtower_containers_scanned":   10,
		"watchtower_containers_updated":   4,
		"watchtower_containers_failed":    2,
		"watchtower_containers_restarted": 3,
		"watchtower_scans_total":          1, // Counter incremented
		"watchtower_scans_skipped_total":  0, // No skips
	}

	for _, mf := range metricFamilies {
		if expectedValue, exists := expectedValues[*mf.Name]; exists {
			if len(mf.Metric) == 0 {
				t.Errorf("No metrics found for %s", *mf.Name)

				continue
			}

			var actualValue float64
			if mf.Metric[0].Gauge != nil {
				actualValue = *mf.Metric[0].Gauge.Value
			} else if mf.Metric[0].Counter != nil {
				actualValue = *mf.Metric[0].Counter.Value
			}

			if actualValue != expectedValue {
				t.Errorf("Metric %s = %v, want %v", *mf.Name, actualValue, expectedValue)
			}
		}
	}

	// Test that restarted metrics don't interfere with other counters
	m.Register(&Metric{Scanned: 5, Updated: 1, Failed: 0, Restarted: 2})
	time.Sleep(10 * time.Millisecond)

	// Re-gather and verify counters incremented correctly
	metricFamilies, err = registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify final state
	finalExpectedValues := map[string]float64{
		"watchtower_containers_scanned":   5, // Latest gauge value
		"watchtower_containers_updated":   1, // Latest gauge value
		"watchtower_containers_failed":    0, // Latest gauge value
		"watchtower_containers_restarted": 2, // Latest gauge value
		"watchtower_scans_total":          2, // Counter incremented again
		"watchtower_scans_skipped_total":  0, // Still no skips
	}

	for _, mf := range metricFamilies {
		if expectedValue, exists := finalExpectedValues[*mf.Name]; exists {
			if len(mf.Metric) == 0 {
				t.Errorf("No metrics found for %s", *mf.Name)

				continue
			}

			var actualValue float64
			if mf.Metric[0].Gauge != nil {
				actualValue = *mf.Metric[0].Gauge.Value
			} else if mf.Metric[0].Counter != nil {
				actualValue = *mf.Metric[0].Counter.Value
			}

			if actualValue != expectedValue {
				t.Errorf("Final metric %s = %v, want %v", *mf.Name, actualValue, expectedValue)
			}
		}
	}

	// Shutdown
	m.Shutdown()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("HandleUpdate did not shutdown within timeout")
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

			t.Cleanup(func() { got.Shutdown() })

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

			if len(metricFamilies) != 8 {
				t.Errorf("Expected 8 metric families registered, got %d", len(metricFamilies))
			}

			expectedNames := map[string]bool{
				"watchtower_containers_scanned":         true,
				"watchtower_containers_updated":         true,
				"watchtower_containers_failed":          true,
				"watchtower_containers_restarted":       true,
				"watchtower_containers_restarted_total": true,
				"watchtower_scans_total":                true,
				"watchtower_scans_skipped_total":        true,
				"watchtower_metrics_dropped_total":      true,
			}

			for _, mf := range metricFamilies {
				if !expectedNames[*mf.Name] {
					t.Errorf("Unexpected metric family registered: %s", *mf.Name)
				}
			}
		})
	}
}

func TestMetrics_RaceConditions(t *testing.T) {
	registry := prometheus.NewRegistry()

	m, err := NewWithRegistry(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	t.Cleanup(func() { m.Shutdown() })

	done := make(chan struct{})

	go func() {
		m.HandleUpdate()
		close(done)
	}()

	// Test concurrent registration of metrics with restarted containers
	const (
		numGoroutines       = 10
		metricsPerGoroutine = 5
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range metricsPerGoroutine {
				metric := &Metric{
					Scanned:   1 + id + j,
					Updated:   id % 2,
					Failed:    (id + j) % 3,
					Restarted: id + j, // Include restarted in race condition test
				}
				m.Register(metric)
				// Small random delay to increase chance of race conditions
				time.Sleep(time.Duration(id+j) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Give time for all metrics to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify that the system didn't crash and metrics were processed
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics after concurrent operations: %v", err)
	}

	// Check that scans_total counter was incremented (at least once per registered metric)
	var totalScans float64

	for _, mf := range metricFamilies {
		if *mf.Name == "watchtower_scans_total" {
			if len(mf.Metric) > 0 && mf.Metric[0].Counter != nil {
				totalScans = *mf.Metric[0].Counter.Value
			}

			break
		}
	}

	expectedMinScans := float64(numGoroutines * metricsPerGoroutine)
	if totalScans < expectedMinScans {
		t.Errorf(
			"Expected at least %v total scans after concurrent operations, got %v",
			expectedMinScans,
			totalScans,
		)
	}

	// Shutdown
	m.Shutdown()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("HandleUpdate did not shutdown within timeout")
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
				metric: &Metric{Scanned: 2, Updated: 1, Failed: 0, Restarted: 0},
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

func TestMetrics_RestartedWithPartialFailures(t *testing.T) {
	registry := prometheus.NewRegistry()

	m, err := NewWithRegistry(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	t.Cleanup(func() { m.Shutdown() })

	// Test scenario where restarted containers have partial failures
	// This simulates a report where some containers were restarted successfully
	// while others failed during the restart process
	testCases := []struct {
		name     string
		metric   *Metric
		expected map[string]float64
	}{
		{
			name: "partial restart success",
			metric: &Metric{
				Scanned:   8,
				Updated:   3,
				Failed:    2,
				Restarted: 3, // 3 out of potentially more were restarted
			},
			expected: map[string]float64{
				"watchtower_containers_scanned":   8,
				"watchtower_containers_updated":   3,
				"watchtower_containers_failed":    2,
				"watchtower_containers_restarted": 3,
			},
		},
		{
			name: "all restarted with failures",
			metric: &Metric{
				Scanned:   5,
				Updated:   0,
				Failed:    3,
				Restarted: 2, // Only 2 restarted despite failures
			},
			expected: map[string]float64{
				"watchtower_containers_scanned":   5,
				"watchtower_containers_updated":   0,
				"watchtower_containers_failed":    3,
				"watchtower_containers_restarted": 2,
			},
		},
		{
			name: "successful restarts only",
			metric: &Metric{
				Scanned:   6,
				Updated:   4,
				Failed:    0,
				Restarted: 2, // Clean restarts without failures
			},
			expected: map[string]float64{
				"watchtower_containers_scanned":   6,
				"watchtower_containers_updated":   4,
				"watchtower_containers_failed":    0,
				"watchtower_containers_restarted": 2,
			},
		},
	}

	done := make(chan struct{})

	go func() {
		m.HandleUpdate()
		close(done)
	}()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m.Register(tc.metric)
			time.Sleep(10 * time.Millisecond)

			// Gather and verify metrics
			metricFamilies, err := registry.Gather()
			if err != nil {
				t.Fatalf("Failed to gather metrics: %v", err)
			}

			for _, mf := range metricFamilies {
				if expectedValue, exists := tc.expected[*mf.Name]; exists {
					if len(mf.Metric) == 0 {
						t.Errorf("No metrics found for %s", *mf.Name)

						continue
					}

					var actualValue float64
					if mf.Metric[0].Gauge != nil {
						actualValue = *mf.Metric[0].Gauge.Value
					}

					if actualValue != expectedValue {
						t.Errorf("Metric %s = %v, want %v", *mf.Name, actualValue, expectedValue)
					}
				}
			}
		})
	}

	// Shutdown
	m.Shutdown()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("HandleUpdate did not shutdown within timeout")
	}
}

func TestMetrics_HandleUpdate(t *testing.T) {
	tests := []struct {
		name string
		m    *Metrics
	}{
		{
			name: "handle valid metric",
			m: func() *Metrics {
				reg := prometheus.NewRegistry()

				return &Metrics{
					channel: make(chan *Metric, 1),
					stopCh:  make(chan struct{}),
					scanned: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_scanned"}),
					updated: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_updated"}),
					failed: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_failed"}),
					restarted: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_restarted"}),
					restartedTotal: promauto.With(reg).
						NewCounter(prometheus.CounterOpts{Name: "test_restarted_total"}),
					total: promauto.With(reg).
						NewCounter(prometheus.CounterOpts{Name: "test_total"}),
					skipped: promauto.With(reg).
						NewCounter(prometheus.CounterOpts{Name: "test_skipped"}),
				}
			}(),
		},
		{
			name: "handle nil metric (skipped)",
			m: func() *Metrics {
				reg := prometheus.NewRegistry()

				return &Metrics{
					channel: make(chan *Metric, 1),
					stopCh:  make(chan struct{}),
					scanned: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_scanned_skip"}),
					updated: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_updated_skip"}),
					failed: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_failed_skip"}),
					restarted: promauto.With(reg).
						NewGauge(prometheus.GaugeOpts{Name: "test_restarted_skip"}),
					restartedTotal: promauto.With(reg).
						NewCounter(prometheus.CounterOpts{Name: "test_restarted_total_skip"}),
					total: promauto.With(reg).
						NewCounter(prometheus.CounterOpts{Name: "test_total_skip"}),
					skipped: promauto.With(reg).
						NewCounter(prometheus.CounterOpts{Name: "test_skipped_skip"}),
				}
			}(),
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
				tt.m.channel <- &Metric{Scanned: 3, Updated: 2, Failed: 1, Restarted: 1}
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
