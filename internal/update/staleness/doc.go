// Package staleness decides which containers an update session replaces.
//
// It reports whether a container should be updated, recognizes images pinned
// by digest, and gathers the metadata of a container's new image for the
// update report, including its confirmed release tag.
package staleness
