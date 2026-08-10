// Package metrics provides tracking and exposure of Watchtower scan metrics.
// It integrates with Prometheus to monitor container scan outcomes.
//
// Key components:
//   - Metrics: Handles metric queuing and updates.
//   - NewMetric: Creates metrics from scan reports.
//
// Usage example:
//
//	// log is the process *zerolog.Logger supplied by the composition root.
//	m := metrics.Default()
//	m.RegisterScan(metrics.NewMetric(report))
//	if !m.QueueIsEmpty() {
//	    log.Info().Msg("Metrics queued")
//	}
//
// The package uses Prometheus for metrics exposure and integrates with types.Report.
package metrics
