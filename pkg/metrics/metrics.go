// Package metrics provides functionality for tracking and exposing Watchtower scan metrics.
// It integrates with Prometheus to monitor container scan outcomes, including scanned, updated, and failed counts.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var metrics *Metrics

// Metric represents the data points collected from a single Watchtower scan.
// It includes counts of scanned, updated, and failed containers.
type Metric struct {
	Scanned int
	Updated int
	Failed  int
}

// Metrics is the handler for processing and exposing scan metrics.
// It maintains a channel for queueing metrics and Prometheus gauges/counters for tracking.
type Metrics struct {
	channel chan *Metric
	scanned prometheus.Gauge
	updated prometheus.Gauge
	failed  prometheus.Gauge
	total   prometheus.Counter
	skipped prometheus.Counter
}

// NewMetric creates a new Metric instance from a types.Report.
// It counts scanned, updated (including stale for compatibility), and failed containers.
func NewMetric(report types.Report) *Metric {
	return &Metric{
		Scanned: len(report.Scanned()),
		// Note: This is for backwards compatibility. ideally, stale containers should be counted separately
		Updated: len(report.Updated()) + len(report.Stale()),
		Failed:  len(report.Failed()),
	}
}

// QueueIsEmpty checks whether any metric messages are currently enqueued in the channel.
// It returns true if the channel is empty, false otherwise.
func (m *Metrics) QueueIsEmpty() bool {
	return len(metrics.channel) == 0
}

// Register enqueues a metric for processing by the metrics handler.
// It sends the metric to the channel for asynchronous handling.
func (m *Metrics) Register(metric *Metric) {
	metrics.channel <- metric
}

// Default creates a new Metrics handler if none exists, or returns the existing singleton.
// It initializes Prometheus gauges and counters, and starts a goroutine to handle metric updates.
func Default() *Metrics {
	if metrics != nil {
		return metrics
	}

	// channelBufferSize defines the buffer capacity for the metrics channel.
	const channelBufferSize = 10

	metrics = &Metrics{
		scanned: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_scanned",
			Help: "Number of containers scanned for changes by watchtower during the last scan",
		}),
		updated: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_updated",
			Help: "Number of containers updated by watchtower during the last scan",
		}),
		failed: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_failed",
			Help: "Number of containers where update failed during the last scan",
		}),
		total: promauto.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_total",
			Help: "Number of scans since the watchtower started",
		}),
		skipped: promauto.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_skipped_total",
			Help: "Number of skipped scans since watchtower started",
		}),
		channel: make(chan *Metric, channelBufferSize),
	}

	go metrics.HandleUpdate(metrics.channel)

	return metrics
}

// RegisterScan fetches the default metrics handler and enqueues a metric for processing.
// It provides a convenient way to register a scan’s metrics without directly accessing the handler.
func RegisterScan(metric *Metric) {
	metrics := Default()
	metrics.Register(metric)
}

// HandleUpdate dequeues metrics from the channel and updates Prometheus metrics accordingly.
// It processes each metric, incrementing counters and setting gauges based on scan outcomes.
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
		// Update metrics with the new values
		m.total.Inc()
		m.scanned.Set(float64(change.Scanned))
		m.updated.Set(float64(change.Updated))
		m.failed.Set(float64(change.Failed))
	}
}
