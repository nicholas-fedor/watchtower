package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
)

// TestDeduplicateEntries verifies that grouped notifications drop
// repeated "Found new image" and "Removing image" entries and keep every other
// entry.
func TestDeduplicateEntries(t *testing.T) {
	t.Parallel()

	t.Run("should return empty slice for empty input", func(t *testing.T) {
		t.Parallel()

		result := deduplicateEntries([]*format.Entry{})
		assert.Empty(t, result)
	})

	t.Run("should return single entry unchanged", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Found new image", Data: map[string]any{"image": "nginx:latest", "new_id": "abc123"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 1)
	})

	t.Run("should deduplicate 'Found new image' entries with same image and new ID", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Found new image", Data: map[string]any{"container": "app-a", "image": "nginx:latest", "new_id": "abc123"}},
			{Message: "Found new image", Data: map[string]any{"container": "app-b", "image": "nginx:latest", "new_id": "abc123"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 1)
		assert.Equal(t, "app-a", result[0].Data["container"])
	})

	t.Run("should keep 'Found new image' entries with different images", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Found new image", Data: map[string]any{"image": "nginx:latest", "new_id": "abc123"}},
			{Message: "Found new image", Data: map[string]any{"image": "redis:latest", "new_id": "def456"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 2)
	})

	t.Run("should keep 'Found new image' entries with same image but different new IDs", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Found new image", Data: map[string]any{"image": "nginx:latest", "new_id": "abc123"}},
			{Message: "Found new image", Data: map[string]any{"image": "nginx:latest", "new_id": "def456"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 2)
	})

	t.Run("should deduplicate 'Removing image' entries with same image ID", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Removing image", Data: map[string]any{"container_name": "app-a", "image_id": "sha256:abc"}},
			{Message: "Removing image", Data: map[string]any{"container_name": "app-b", "image_id": "sha256:abc"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 1)
		assert.Equal(t, "app-a", result[0].Data["container_name"])
	})

	t.Run("should not deduplicate other message types", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Stopping container", Data: map[string]any{"container": "app-a"}},
			{Message: "Stopping container", Data: map[string]any{"container": "app-b"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 2)
	})

	t.Run("should handle mixed entry types correctly", func(t *testing.T) {
		t.Parallel()

		entries := []*format.Entry{
			{Message: "Found new image", Data: map[string]any{"image": "nginx:latest", "new_id": "abc123"}},
			{Message: "Stopping container", Data: map[string]any{"container": "app-a"}},
			{Message: "Found new image", Data: map[string]any{"image": "nginx:latest", "new_id": "abc123"}},
			{Message: "Removing image", Data: map[string]any{"image_id": "sha256:old"}},
			{Message: "Started new container", Data: map[string]any{"container": "app-a"}},
			{Message: "Removing image", Data: map[string]any{"image_id": "sha256:old"}},
		}
		result := deduplicateEntries(entries)
		require.Len(t, result, 4)
		assert.Equal(t, "Found new image", result[0].Message)
		assert.Equal(t, "Stopping container", result[1].Message)
		assert.Equal(t, "Removing image", result[2].Message)
		assert.Equal(t, "Started new container", result[3].Message)
	})
}
