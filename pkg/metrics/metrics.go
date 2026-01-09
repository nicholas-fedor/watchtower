package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var metrics *Metrics

// Metric holds data points from a Watchtower scan.
type Metric struct {
	Scanned   int // Number of containers scanned.
	Updated   int // Number of containers updated (excludes stale).
	Failed    int // Number of containers failed.
	Restarted int // Number of containers restarted due to linked dependencies.
}

// Metrics handles processing and exposing scan metrics.
type Metrics struct {
	channel        chan *Metric       // Channel for queuing metrics.
	scanned        prometheus.Gauge   // Gauge for scanned containers.
	updated        prometheus.Gauge   // Gauge for updated containers.
	failed         prometheus.Gauge   // Gauge for failed containers.
	restarted      prometheus.Gauge   // Gauge for restarted containers.
	restartedTotal prometheus.Counter // Counter for total restarted containers.
	total          prometheus.Counter // Counter for total scans.
	skipped        prometheus.Counter // Counter for skipped scans.
	dropped        prometheus.Counter // Counter for dropped metrics.
	stopCh         chan struct{}      // Channel for shutdown signaling.
	shutdownOnce   sync.Once          // Ensures shutdown is called only once.
	//nolint:containedctx
	ctx    context.Context    // Context for cancellation.
	cancel context.CancelFunc // Cancel function for the context.
}

// NewWithRegistry creates a new Metrics handler with a custom Prometheus registry.
//
// Parameters:
//   - registry: Prometheus registerer to use for metric registration.
//
// Returns:
//   - (*Metrics, error): Metrics handler with Prometheus metrics and goroutine, or an error if registration fails.
func NewWithRegistry(registry prometheus.Registerer) (*Metrics, error) {
	// channelBufferSize sets the metrics channel capacity.
	const channelBufferSize = 10

	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())

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
		restarted: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_restarted",
			Help: "Number of containers restarted due to linked dependencies during the last scan",
		}),
		restartedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_containers_restarted_total",
			Help: "Total number of containers restarted due to linked dependencies",
		}),
		total: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_total",
			Help: "Number of scans since the watchtower started",
		}),
		skipped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_skipped_total",
			Help: "Number of skipped scans since watchtower started",
		}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_metrics_dropped_total",
			Help: "Number of metrics dropped due to full channel",
		}),
		channel: make(chan *Metric, channelBufferSize),
		stopCh:  make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Register the metrics with the provided registry.
	// If a metric is already registered, return an error to avoid duplicate collectors.
	metricsList := []prometheus.Collector{
		metrics.scanned,
		metrics.updated,
		metrics.failed,
		metrics.restarted,
		metrics.restartedTotal,
		metrics.total,
		metrics.skipped,
		metrics.dropped,
	}
	for _, m := range metricsList {
		err := registry.Register(m)
		if err != nil {
			alreadyRegisteredError := &prometheus.AlreadyRegisteredError{}
			if errors.As(err, &alreadyRegisteredError) {
				return nil, fmt.Errorf("failed to register metric: %w", err)
			}
		}
	}

	// Start goroutine to process metrics.
	go metrics.HandleUpdate()

	return metrics, nil
}

// NewMetric creates a Metric from a scan report.
//
// Parameters:
//   - report: Scan report from types.Report.
//
// Returns:
//   - *Metric: New metric instance.
func NewMetric(report types.Report) *Metric {
	if report == nil {
		panic("NewMetric: report is nil")
	}

	return &Metric{
		Scanned:   len(report.Scanned()),
		Updated:   len(report.Updated()), // Only count actually updated containers.
		Failed:    len(report.Failed()),
		Restarted: len(report.Restarted()),
	}
}

// QueueIsEmpty checks if the metrics channel is empty.
//
// Returns:
//   - bool: True if empty, false otherwise.
func (m *Metrics) QueueIsEmpty() bool {
	return len(m.channel) == 0
}

// Register attempts to enqueue a metric for processing.
// If the channel is full, the metric is dropped and the dropped counter is incremented.
//
// Parameters:
//   - metric: Metric to register.
func (m *Metrics) Register(metric *Metric) {
	select {
	case m.channel <- metric:
		// Metric sent successfully
	default:
		// Channel is full, drop the metric
		m.dropped.Inc()
	}
}

// Default initializes or returns the singleton Metrics handler. It panics on registration failure, such as duplicate registration against the default registry.
//
// Returns:
//   - *Metrics: Metrics handler with Prometheus metrics and goroutine.
func Default() *Metrics {
	if metrics != nil {
		return metrics
	}

	var err error

	metrics, err = NewWithRegistry(prometheus.DefaultRegisterer)
	if err != nil {
		panic(err)
	}

	return metrics
}

// RegisterScan enqueues a scan metric using the default handler.
//
// Parameters:
//   - metric: Metric to register.
func (m *Metrics) RegisterScan(metric *Metric) {
	m.Register(metric)
}

// Shutdown gracefully stops the metrics processing goroutine.
// It closes the stopCh channel and cancels the context to signal the goroutine to exit.
// This method is idempotent and can be called multiple times safely.
func (m *Metrics) Shutdown() {
	m.shutdownOnce.Do(func() {
		close(m.stopCh)
		m.cancel()
	})
}

// HandleUpdate processes metrics from the channel.
func (m *Metrics) HandleUpdate() {
	for {
		select {
		case change, ok := <-m.channel:
			if !ok {
				// Channel closed: exit handler.
				return
			}

			if change == nil {
				// Update was skipped and rescheduled
				m.total.Inc()
				m.skipped.Inc()
				m.scanned.Set(0)
				m.updated.Set(0)
				m.failed.Set(0)
				m.restarted.Set(0)

				continue
			}
			// Update metrics with scan results.
			m.total.Inc()
			m.scanned.Set(float64(change.Scanned))
			m.updated.Set(float64(change.Updated))
			m.failed.Set(float64(change.Failed))
			m.restarted.Set(float64(change.Restarted))
			m.restartedTotal.Add(float64(change.Restarted))
		case <-m.stopCh:
			return
		case <-m.ctx.Done():
			return
		}
	}
}
