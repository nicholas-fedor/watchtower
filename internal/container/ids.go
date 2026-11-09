package container

import (
	"strings"
)

// WatchtowerOldPrefix is the prefix used when renaming Watchtower containers
// during self-update. It is the single source of truth for both rename
// generation and old-name detection to prevent cross-file protocol drift.
const WatchtowerOldPrefix = "watchtower-old-"

// ImageID is a hash string for a container image.
type ImageID string

// ContainerID is a hash string for a container instance.
type ContainerID string

// ShortID returns the 12-character short version of an image ID.
//
// Returns:
//   - string: Shortened ID without "sha256:" prefix.
func (id ImageID) ShortID() string {
	return shortID(string(id))
}

// ShortID returns the 12-character short version of a container ID.
//
// Returns:
//   - string: Shortened ID without "sha256:" prefix.
func (id ContainerID) ShortID() string {
	return shortID(string(id))
}

// shortID shortens a hash string to 12 characters.
//
// Parameters:
//   - longID: Full hash string.
//
// Returns:
//   - string: Shortened ID, adjusted for "sha256:" prefix.
func shortID(longID string) string {
	prefixSep := strings.IndexRune(longID, ':')
	offset := 0
	length := 12

	// Adjust offset for "sha256:" prefix.
	if prefixSep >= 0 {
		if longID[0:prefixSep] == "sha256" {
			offset = prefixSep + 1
		} else {
			length += prefixSep + 1
		}
	}

	// Return shortened ID or full string if too short.
	if len(longID) >= offset+length {
		return longID[offset : offset+length]
	}

	return longID
}
