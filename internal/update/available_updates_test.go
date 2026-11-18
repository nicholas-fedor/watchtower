package update

import (
	"errors"
	"testing"
	"testing/synctest"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// Test_newScanMetric_UpdatesAvailable checks the Prometheus output of session
// reports, independently of whether their available updates were applied.
func Test_newScanMetric_UpdatesAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := prometheus.NewRegistry()
		m, err := metrics.NewWithRegistry(registry)
		require.NoError(t, err)
		t.Cleanup(m.Shutdown)

		log := new(zerolog.Nop())
		failed, _ := reportfixture.Container(0, state.FailedState)
		unknown := state.Progress{}
		unknown.AddScanned(log, failed, "", updateParams.Params{})
		unknown.UpdateFailed(log, map[container.ContainerID]error{
			failed.ID(): errors.New("image lookup failed"),
		})

		skipped, latest := reportfixture.Container(0, state.SkippedState)
		deferred := state.Progress{}
		deferred.AddSkipped(log, skipped, errors.New("dependency failed"), updateParams.Params{})
		deferred.SetLatestImage(log, skipped.ID(), latest)

		for _, tt := range []struct {
			report    *state.Report
			name      string
			available int
			updated   int
		}{
			{reportfixture.Report(state.StaleState, state.FreshState), "monitor only", 1, 0},
			{reportfixture.Report(state.UpdatedState), "updated", 1, 1},
			{reportfixture.Report(state.UpdatedState, state.FailedState, state.StaleState), "mixed outcomes", 3, 1},
			{deferred.Report(log), "known update skipped", 1, 0},
			{unknown.Report(log), "unknown image after failed lookup", 0, 0},
			{reportfixture.Report(state.RestartedState), "restart without new image", 0, 0},
			{reportfixture.Report(state.FreshState, state.SkippedState), "no available updates", 0, 0},
			{reportfixture.Report(), "empty scan", 0, 0},
		} {
			m.RegisterScan(newScanMetric(tt.report))
			synctest.Wait()

			families, err := registry.Gather()
			require.NoError(t, err)

			values := make(map[string]float64)

			for _, family := range families {
				series := family.GetMetric()
				if len(series) == 1 && series[0].GetGauge() != nil {
					values[family.GetName()] = series[0].GetGauge().GetValue()
				}
			}

			require.Contains(t, values, "watchtower_updates_available", tt.name)
			require.InDelta(t, tt.available, values["watchtower_updates_available"], 0, tt.name)
			require.InDelta(t, tt.updated, values["watchtower_containers_updated"], 0, tt.name)
		}
	})
}
