package notifications

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
//   - []*notificationEntry: Deduplicated entries preserving original order.
func deduplicateEntries(entries []*notificationEntry) []*notificationEntry {
	if len(entries) <= 1 {
		return entries
	}

	type dedupKey struct {
		message string
		data    string
	}

	seen := make(map[dedupKey]bool)
	result := make([]*notificationEntry, 0, len(entries))

	for _, entry := range entries {
		var key dedupKey

		switch entry.Message {
		case "Found new image":
			// Deduplicate by image name and new image ID.
			image, _ := entry.Data["image"].(string)
			newID, _ := entry.Data["new_id"].(string)
			key = dedupKey{message: entry.Message, data: image + "\x00" + newID}
		case "Found new Git revision":
			revision, _ := entry.Data["revision"].(string)
			shortCommit, _ := entry.Data["short_commit"].(string)
			key = dedupKey{message: entry.Message, data: revision + "\x00" + shortCommit}
		case "Removing image":
			// Deduplicate by image ID.
			imageID, _ := entry.Data["image_id"].(string)
			key = dedupKey{message: entry.Message, data: imageID}
		case "Image is within cooldown period - not eligible for update",
			"Image age exceeds cooldown - eligible for update",
			"Image creation time unavailable - update check unavailable":
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
