package update

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// cooldownConfig returns a base [updateParams.Params] with CooldownDelay and standard settings.
func cooldownConfig(cooldownDelay time.Duration) updateParams.Params {
	return updateParams.Params{
		Cleanup:       true,
		Filter:        filters.NoFilter,
		CooldownDelay: cooldownDelay,
	}
}

// staleCheckFailures returns the failed count from the staleness check summary
// in JSON log output, failing the test when no summary was logged.
func staleCheckFailures(t *testing.T, logs string) int {
	t.Helper()

	// summary holds the fields the test reads from each log line.
	type summary struct {
		Message string `json:"message"`
		Failed  int    `json:"failed"`
	}

	for _, entry := range logcapture.Entries[summary](t, logs) {
		if entry.Message == "Completed container staleness check" {
			return entry.Failed
		}
	}

	require.Fail(t, "no staleness check summary was logged")

	return 0
}

// TestUpdateAction_Cooldown verifies that an update skips a container whose
// image is in its cooldown or whose image age is unknown, and updates it once
// the cooldown has passed, when the cooldown is zero, or when pulls are off.
// Monitor-only mode updates nothing.
func TestUpdateAction_Cooldown(t *testing.T) {
	t.Parallel()

	t.Run("CooldownDelay is set and image is newer than cooldown", func(t *testing.T) {
		t.Parallel()

		t.Run("should defer the update and skip the container", func(t *testing.T) {
			t.Parallel()

			eligibleAt := time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)

			client := mockUpdate.MockClient{
				TestData: &mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer("c1", "c1", "myimage:latest", time.Now()),
					},
					IsContainerStaleError: &containerImage.CooldownError{
						EligibleAt: eligibleAt,
						Age:        "20 minutes",
						Delay:      "1 hour",
						Remaining:  "40 minutes",
					},
				},
				Stopped: make(map[string]bool),
			}

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)

			report, _, err := update(log, t.Context(), client, cooldownConfig(1*time.Hour), ratelimit.New())

			require.NoError(t, err)
			assert.Equal(t, 0, staleCheckFailures(t, buf.String()), "a deferral is not a failed check")
			require.Len(t, report.Skipped(), 1, "Container should be skipped due to cooldown")

			skipped := report.Skipped()[0]
			assert.Equal(t, "1 hour", skipped.CooldownDelay())
			assert.Equal(t, "20 minutes", skipped.CooldownAge())
			assert.Equal(t, "40 minutes", skipped.CooldownRemaining())
			assert.Equal(t, eligibleAt, skipped.CooldownEligibleAt())
			assert.Empty(t, report.Scanned(), "Container should not be scanned when deferred by cooldown")
			assert.Empty(t, report.Updated(), "Container should not be updated when deferred by cooldown")
		})
	})

	t.Run("CooldownDelay is set and image is older than cooldown", func(t *testing.T) {
		t.Parallel()

		t.Run("should proceed with the update normally", func(t *testing.T) {
			t.Parallel()

			// The client decides cooldown during its staleness check, so an image
			// past its cooldown reports stale without an error.
			client := mockUpdate.MockClient{
				TestData: &mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer("c1", "c1", "myimage:latest", time.Now()),
					},
					Staleness: map[string]bool{
						"c1": true,
					},
				},
				Stopped: make(map[string]bool),
			}

			report, _, err := update(newTestLogger(t), t.Context(), client, cooldownConfig(1*time.Hour), ratelimit.New())

			require.NoError(t, err)
			assert.Empty(t, report.Skipped(), "Container should not be skipped when cooldown has elapsed")
			require.Len(t, report.Updated(), 1, "Container should be updated after cooldown passes")
		})
	})

	t.Run("CooldownDelay is zero", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip the cooldown check entirely", func(t *testing.T) {
			t.Parallel()

			// No mock registry server is needed because the cooldown check
			// is skipped when CooldownDelay is zero.
			client := mockUpdate.MockClient{
				TestData: &mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer("c1", "c1", "fake-image:latest", time.Now()),
					},
					Staleness: map[string]bool{
						"c1": true,
					},
				},
				Stopped: make(map[string]bool),
			}

			report, _, err := update(newTestLogger(t), t.Context(), client, cooldownConfig(0), ratelimit.New())

			require.NoError(t, err)
			assert.Empty(t, report.Skipped(), "Container should not be skipped when cooldown is disabled")
			require.Len(t, report.Updated(), 1, "Container should be updated when cooldown is disabled")
		})
	})

	t.Run("NoPull is enabled", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip the cooldown check", func(t *testing.T) {
			t.Parallel()

			// No mock registry server is needed because NoPull bypasses the cooldown check.
			// Use a standard mock container via CreateMockContainer.
			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer("c1", "c1", "fake-image:latest", time.Now()),
					},
					Staleness: map[string]bool{
						"c1": true,
					},
				},
				false,
				false,
			)

			config := cooldownConfig(1 * time.Hour)
			config.NoPull = true

			report, _, err := update(newTestLogger(t), t.Context(), client, config, ratelimit.New())

			require.NoError(t, err)
			assert.Empty(t, report.Skipped(), "Container should not be skipped when NoPull is set")
			require.Len(t, report.Updated(), 1, "Container should be updated when NoPull bypasses cooldown")
		})
	})

	t.Run("image age fetch fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip the container with a conservative posture", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.MockClient{
				TestData: &mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer("c1", "c1", "myimage:latest", time.Now()),
					},
					// A failed age lookup carries the configured delay and the
					// lookup's error.
					IsContainerStaleError: &containerImage.CooldownError{
						Delay: "1 hour",
						Cause: errors.New("registry unreachable"),
					},
				},
				Stopped: make(map[string]bool),
			}

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)

			report, _, err := update(log, t.Context(), client, cooldownConfig(1*time.Hour), ratelimit.New())

			require.NoError(t, err)
			assert.Equal(t, 1, staleCheckFailures(t, buf.String()), "a failed age lookup is a failed check")
			require.Len(t, report.Skipped(), 1, "Container should be skipped when image age fetch fails")

			skipped := report.Skipped()[0]
			assert.Equal(t, "1 hour", skipped.CooldownDelay())
			assert.Empty(t, skipped.CooldownRemaining())
			assert.Empty(t, report.Updated(), "Container should not be updated when image age fetch fails")
		})
	})

	t.Run("MonitorOnly is enabled", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip the cooldown check entirely", func(t *testing.T) {
			t.Parallel()

			// No mock registry server is needed because monitor-only bypasses the cooldown check.
			// Use a standard mock container via CreateMockContainer.
			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer("c1", "c1", "fake-image:latest", time.Now()),
					},
					Staleness: map[string]bool{
						"c1": true,
					},
				},
				false,
				false,
			)

			config := cooldownConfig(1 * time.Hour)
			config.MonitorOnly = true

			report, _, err := update(newTestLogger(t), t.Context(), client, config, ratelimit.New())

			require.NoError(t, err)
			assert.Empty(t, report.Skipped(), "Container should not be skipped when MonitorOnly is set")
			assert.Empty(t, report.Updated(), "Monitor-only container should not be updated")
		})
	})
}
