package metrics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var metrics *Metrics

// Metric holds data points from a Watchtower scan.
type Metric struct {
	Scanned   int // Number of containers scanned.
	Updated   int // Number of containers updated (excludes stale).
	Failed    int // Number of containers failed.
	Restarted int // Number of containers restarted due to linked dependencies.
	Skipped   int // Number of containers skipped during the scan.
}

// Metrics handles processing and exposing scan metrics.
type Metrics struct {
	channel        chan *Metric       // Channel for queuing metrics.
	scanned        prometheus.Gauge   // Gauge for scanned containers.
	updated        prometheus.Gauge   // Gauge for updated containers.
	failed         prometheus.Gauge   // Gauge for failed containers.
	restarted      prometheus.Gauge   // Gauge for restarted containers.
	skipped        prometheus.Gauge   // Gauge for skipped containers.
	restartedTotal prometheus.Counter // Counter for total restarted containers.
	total          prometheus.Counter // Counter for total scans.
	skippedScans   prometheus.Counter // Counter for skipped scans.
	dropped        prometheus.Counter // Counter for dropped metrics.
	stopCh         chan struct{}      // Channel for shutdown signaling.
	shutdownOnce   sync.Once          // Ensures shutdown is called only once.
	lastMetric     *Metric            // Last scan metric for status endpoint.
	lastMetricMu   sync.RWMutex       // Protects lastMetric.
	history        []HistoryEntry     // Ring buffer of scan history.
	historyIdx     int                // Current write position in the ring buffer.
	historyMu      sync.RWMutex       // Protects history and historyIdx.
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
		skipped: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "watchtower_containers_skipped",
			Help: "Number of containers skipped during the last scan",
		}),
		restartedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_containers_restarted_total",
			Help: "Total number of containers restarted due to linked dependencies",
		}),
		total: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchtower_scans_total",
			Help: "Number of scans since the watchtower started",
		}),
		skippedScans: prometheus.NewCounter(prometheus.CounterOpts{
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
		metrics.skipped,
		metrics.restartedTotal,
		metrics.total,
		metrics.skippedScans,
		metrics.dropped,
	}
	for _, m := range metricsList {
		err := registry.Register(m)
		if err != nil {
			alreadyRegisteredError := &prometheus.AlreadyRegisteredError{}
			if errors.As(err, &alreadyRegisteredError) {
				return nil, fmt.Errorf("failed to register metric: %w", err)
			}

			logrus.WithError(err).Warn("failed to register metric")
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
		Skipped:   len(report.Skipped()),
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

// GetLastScan returns a copy of the most recent scan metric, or nil if no scan
// has completed yet. The returned copy is safe to modify by the caller without
// affecting the internal metrics state.
//
// Returns:
//   - *Metric: Copy of the last scan metric, or nil.
func (m *Metrics) GetLastScan() *Metric {
	m.lastMetricMu.RLock()
	defer m.lastMetricMu.RUnlock()

	if m.lastMetric == nil {
		return nil
	}

	metricCopy := *m.lastMetric

	return &metricCopy
}

// historyBufferSize defines the maximum number of scan history entries
// retained in the in-memory ring buffer.
const historyBufferSize = 500

// HistoryEntry represents a single scan record in the history ring buffer.
type HistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Scanned   int       `json:"scanned"`
	Updated   int       `json:"updated"`
	Failed    int       `json:"failed"`
	Restarted int       `json:"restarted"`
	Skipped   int       `json:"skipped"`
}

// RecordHistory stores a scan result in the ring buffer.
// When the buffer is full, the oldest entry is overwritten.
func (m *Metrics) RecordHistory(metric *Metric) {
	m.historyMu.Lock()
	defer m.historyMu.Unlock()

	entry := HistoryEntry{
		Timestamp: time.Now().UTC(),
		Scanned:   metric.Scanned,
		Updated:   metric.Updated,
		Failed:    metric.Failed,
		Restarted: metric.Restarted,
		Skipped:   metric.Skipped,
	}

	if len(m.history) < historyBufferSize {
		m.history = append(m.history, entry)
	} else {
		m.history[m.historyIdx] = entry
		m.historyIdx = (m.historyIdx + 1) % historyBufferSize
	}
}

// GetHistory returns a copy of the history entries, optionally filtered
// by since/until timestamps and limited to the most recent N entries.
// The returned slice is sorted by timestamp in ascending order.
func (m *Metrics) GetHistory(since, until *time.Time, limit int) []HistoryEntry {
	m.historyMu.RLock()
	defer m.historyMu.RUnlock()

	result := make([]HistoryEntry, 0, len(m.history))

	for _, entry := range m.history {
		if since != nil && entry.Timestamp.Before(*since) {
			continue
		}

		if until != nil && entry.Timestamp.After(*until) {
			continue
		}

		result = append(result, entry)
	}

	slices.SortFunc(result, func(a, b HistoryEntry) int {
		return a.Timestamp.Compare(b.Timestamp)
	})

	if limit > 0 && limit < len(result) {
		result = result[len(result)-limit:]
	}

	return result
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
				m.skippedScans.Inc()
				m.scanned.Set(0)
				m.updated.Set(0)
				m.failed.Set(0)
				m.restarted.Set(0)
				m.skipped.Set(0)

				continue
			}
			// Update metrics with scan results.
			m.total.Inc()
			m.scanned.Set(float64(change.Scanned))
			m.updated.Set(float64(change.Updated))
			m.failed.Set(float64(change.Failed))
			m.restarted.Set(float64(change.Restarted))
			m.skipped.Set(float64(change.Skipped))
			m.restartedTotal.Add(float64(change.Restarted))

			m.lastMetricMu.Lock()
			m.lastMetric = change
			m.lastMetricMu.Unlock()

			m.RecordHistory(change)
		case <-m.stopCh:
			return
		case <-m.ctx.Done():
			return
		}
	}
}
