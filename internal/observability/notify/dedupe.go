package notify

import (
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
)

// deduplicateEntries removes duplicate log entries that occur when multiple containers
// share the same image. This applies to grouped (non-split) notifications only.
//
// For "Found new image" entries, deduplication is based on (message, image name, new image ID).
// When new_id is a short registry digest (check path) rather than a local image ID,
// entries still dedupe by image + new_id so multiple containers on the same image
// produce one notification in grouped mode.
// For "Removing image" entries, deduplication is based on (message, image ID).
// All other entries are kept as-is.
//
// Parameters:
//   - entries: Log entries that may contain duplicates.
//
// Returns:
//   - []*format.Entry: Deduplicated entries preserving original order.
func deduplicateEntries(entries []*format.Entry) []*format.Entry {
	if len(entries) <= 1 {
		return entries
	}

	type dedupKey struct {
		message string
		data    string
	}

	seen := make(map[dedupKey]bool)
	result := make([]*format.Entry, 0, len(entries))

	for _, entry := range entries {
		var key dedupKey

		switch entry.Message {
		case logging.FoundNewImage:
			// Deduplicate by image name and new image ID.
			image, _ := entry.Data["image"].(string)
			newID, _ := entry.Data["new_id"].(string)
			key = dedupKey{message: entry.Message, data: image + "\x00" + newID}
		case logging.FoundNewGitRevision:
			revision, _ := entry.Data["revision"].(string)
			shortCommit, _ := entry.Data["short_commit"].(string)
			key = dedupKey{message: entry.Message, data: revision + "\x00" + shortCommit}
		case logging.RemovingImage:
			// Deduplicate by image ID.
			imageID, _ := entry.Data["image_id"].(string)
			key = dedupKey{message: entry.Message, data: imageID}
		case logging.WithinCooldown,
			logging.CooldownElapsed,
			logging.CooldownUnavailable:
			// Deduplicate by image name — multiple containers may share the same image.
			image, _ := entry.Data["image"].(string)
			key = dedupKey{message: entry.Message, data: image}
		default:
			// Non-deduplicatable entries are always kept.
			result = append(result, entry)

			continue
		}

		if !seen[key] {
			seen[key] = true

			result = append(result, entry)
		}
	}

	return result
}
