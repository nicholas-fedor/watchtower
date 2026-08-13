package preview

import (
	"testing"

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
