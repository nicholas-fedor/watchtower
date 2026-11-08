package preview

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataForMessageKnownFields(t *testing.T) {
	t.Parallel()

	generator := New()

	got := generator.dataForMessage("Found new image", "web", "org/web:latest")
	require.NotNil(t, got)
	assert.Equal(t, "org/web:latest", got["image"])
	assert.NotEmpty(t, got["new_id"])

	got = generator.dataForMessage("Watchtower v1.11.7 using Docker API v1.51", "web", "org/web:latest")
	assert.Nil(t, got)

	got = generator.dataForMessage(
		"Docker image usage exceeds configured maximum",
		"web",
		"org/web:latest",
	)
	require.NotNil(t, got)
	assert.Equal(t, previewDiskMax, got["usage"])
	assert.Equal(t, previewDiskMax, got["max"])
	assert.Equal(t, previewDiskWarn, got["warn"])
	assert.Equal(t, previewDiskReclaimable, got["reclaimable"])
	assert.Equal(t, previewDiskImageCount, got["image_count"])

	got = generator.dataForMessage(
		"Docker image usage exceeds configured warning threshold",
		"web",
		"org/web:latest",
	)
	require.NotNil(t, got)
	assert.Equal(t, previewDiskUsage, got["usage"])
	assert.Equal(t, previewDiskMax, got["max"])
	assert.Equal(t, previewDiskWarn, got["warn"])

	got = generator.dataForMessage("Failed to query Docker image disk usage", "web", "org/web:latest")
	require.NotNil(t, got)
	assert.Equal(t, "daemon disk usage unavailable", got["error"])

	got = generator.dataForMessage("Docker image usage budget enabled", "web", "org/web:latest")
	require.NotNil(t, got)
	assert.Equal(t, previewDiskMax, got["disk_space_max"])
	assert.Equal(t, previewDiskWarn, got["disk_space_warn"])
}

// TestAddFromStateCooldown verifies that the first skipped container waits out
// an image cooldown. It carries its cooldown fields and its running image as
// the latest. Later skipped containers carry no cooldown fields.
func TestAddFromStateCooldown(t *testing.T) {
	t.Parallel()

	generator := New()
	generator.AddFromState(SkippedState)
	generator.AddFromState(SkippedState)

	skipped := generator.NotificationData().Report.Skipped()
	require.Len(t, skipped, 2)

	held := skipped[0]
	assert.Equal(t, "image is within cooldown period", held.Error())
	assert.Equal(t, held.CurrentImageID(), held.LatestImageID())
	assert.Equal(t, "18 hours", held.CooldownAge())
	assert.Equal(t, "1 day", held.CooldownDelay())
	assert.Equal(t, "6 hours", held.CooldownRemaining())
	assert.Equal(t, previewStartTime.Add(6*time.Hour), held.CooldownEligibleAt())

	other := skipped[1]
	assert.NotEqual(t, "image is within cooldown period", other.Error())
	assert.Empty(t, other.CooldownDelay())
	assert.Empty(t, other.CooldownAge())
	assert.Empty(t, other.CooldownRemaining())
	assert.True(t, other.CooldownEligibleAt().IsZero())
}

// TestAddFromStateSkippedReason verifies that a skipped container without a
// cooldown names the skip once, followed by one of the sample reasons. Each
// container gets a random reason. The test also checks every sample directly.
func TestAddFromStateSkippedReason(t *testing.T) {
	t.Parallel()

	for _, reason := range skippedMessages {
		assert.NotContains(t, reason, "container skipped")
	}

	generator := New()
	generator.AddFromState(SkippedState) // The first skipped container waits out a cooldown.
	generator.AddFromState(SkippedState)

	skipped := generator.NotificationData().Report.Skipped()
	require.Len(t, skipped, 2)

	got := skipped[1].Error()
	reason, found := strings.CutPrefix(got, "container skipped: ")
	require.True(t, found, "error %q", got)
	assert.NotContains(t, reason, "container skipped", "error %q", got)
	assert.Contains(t, skippedMessages, reason)
}

// TestAddFromStateScannedList verifies that the scanned list holds every sample
// that is not skipped, each in its own state, as Watchtower's scanned list
// holds every container it did not skip. A skipped sample is listed only as
// skipped.
func TestAddFromStateScannedList(t *testing.T) {
	t.Parallel()

	generator := New()
	for _, state := range []State{UpdatedState, FailedState, SkippedState, RestartedState, StaleState, FreshState} {
		generator.AddFromState(state)
	}

	report := generator.NotificationData().Report

	scanned := make([]string, 0, len(report.Scanned()))
	for _, status := range report.Scanned() {
		scanned = append(scanned, status.State())
	}

	assert.ElementsMatch(t, []string{"Updated", "Failed", "Restarted", "Stale", "Fresh"}, scanned)
	assert.Len(t, report.Skipped(), 1)
	assert.Len(t, report.All(), 6)
}

func TestNotificationDataReportPresence(t *testing.T) {
	t.Parallel()

	generator := New()
	payload := generator.NotificationData()
	assert.Nil(t, payload.Report)

	generator.AddFromState(UpdatedState)
	payload = generator.NotificationData()
	require.NotNil(t, payload.Report)
	assert.Len(t, payload.Report.Updated(), 1)
	assert.Equal(t, "Title", payload.Title)
	assert.Equal(t, "Host", payload.Host)
}
