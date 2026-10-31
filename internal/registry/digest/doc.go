// Package digest fetches image digests from container registries and compares
// them with local image digests to decide whether a container's image is stale.
// It also reads an image's creation time from its registry for the image
// cooldown.
package digest
