// Package metrics provides functionality for tracking and exposing Watchtower scan metrics.
// It integrates with Prometheus to monitor container scan outcomes, including scanned, updated, and failed counts.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var metrics *Metrics

// Metric holds data points from a Watchtower scan.
type Metric struct {
	Scanned int // Number of containers scanned.
	Updated int // Number of containers updated (excludes stale).
	Failed  int // Number of containers failed.
}

// Metrics handles processing and exposing scan metrics.
type Metrics struct {
	channel chan *Metric       // Channel for queuing metrics.
	scanned prometheus.Gauge   // Gauge for scanned containers.
	updated prometheus.Gauge   // Gauge for updated containers.
	failed  prometheus.Gauge   // Gauge for failed containers.
	total   prometheus.Counter // Counter for total scans.
	skipped prometheus.Counter // Counter for skipped scans.
}

// NewMetric creates a Metric from a scan report.
// NewWithRegistry creates a new Metrics handler with a custom Prometheus registry.
//
// Parameters:
//   - registry: Prometheus registerer to use for metric registration.
//
// Returns:
//   - *Metrics: Metrics handler with Prometheus metrics and goroutine.
func NewWithRegistry(registry prometheus.Registerer) *Metrics {
	// channelBufferSize sets the metrics channel capacity.
	const channelBufferSize = 10

	// Initialize metrics with Prometheus gauges and counters.
	metrics := &Metrics{
		scanned: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_scanned",
			Help: "Number of containers scanned for changes by watchtower during the last scan",
		}),
		updated: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_updated",
			Help: "Number of containers updated by watchtower during the last scan",
		}),
		failed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_failed",
			Help: "Number of containers where update failed during the last scan",
		}),
		total: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_total",
			Help: "Number of scans since the watchtower started",
		}),
		skipped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_skipped_total",
			Help: "Number of skipped scans since watchtower started",
		}),
		channel: make(chan *Metric, channelBufferSize),
	}

	// Register the metrics with the provided registry.
	registry.MustRegister(
		metrics.scanned,
		metrics.updated,
		metrics.failed,
		metrics.total,
		metrics.skipped,
	)

	// Start goroutine to process metrics.
	go metrics.HandleUpdate(metrics.channel)

	return metrics
}

// NewMetric creates a Metric from a scan report.
//
// Parameters:
//   - report: Scan report from types.Report.
//
// Returns:
//   - *Metric: New metric instance.
func NewMetric(report types.Report) *Metric {
	return &Metric{
		Scanned: len(report.Scanned()),
		Updated: len(report.Updated()), // Only count actually updated containers.
		Failed:  len(report.Failed()),
	}
}

// QueueIsEmpty checks if the metrics channel is empty.
//
// Returns:
//   - bool: True if empty, false otherwise.
func (m *Metrics) QueueIsEmpty() bool {
	return len(m.channel) == 0
}

// Register enqueues a metric for processing.
//
// Parameters:
//   - metric: Metric to register.
func (m *Metrics) Register(metric *Metric) {
	m.channel <- metric
}

// Default initializes or returns the singleton Metrics handler.
//
// Returns:
//   - *Metrics: Metrics handler with Prometheus metrics and goroutine.
func Default() *Metrics {
	if metrics != nil {
		return metrics
	}

	metrics = NewWithRegistry(prometheus.DefaultRegisterer)

	return metrics
}

// RegisterScan enqueues a scan metric using the default handler.
//
// Parameters:
//   - metric: Metric to register.
func (m *Metrics) RegisterScan(metric *Metric) {
	m.Register(metric)
}

// HandleUpdate processes metrics from the channel.
//
// Parameters:
//   - channel: Channel to dequeue metrics from.
func (m *Metrics) HandleUpdate(channel <-chan *Metric) {
	for change := range channel {
		if change == nil {
			// Update was skipped and rescheduled
			m.total.Inc()
			m.skipped.Inc()
			m.scanned.Set(0)
			m.updated.Set(0)
			m.failed.Set(0)

			continue
		}
		// Update metrics with scan results.
		m.total.Inc()
		m.scanned.Set(float64(change.Scanned))
		m.updated.Set(float64(change.Updated))
		m.failed.Set(float64(change.Failed))
	}
}
