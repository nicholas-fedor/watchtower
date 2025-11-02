// Package types provides core types for Watchtower operations.
package types

// CleanedImageInfo represents information about an image that was cleaned up during update operations.
// It tracks the image ID, image name, and the container that was using the old image before cleanup.
type CleanedImageInfo struct {
	// ImageID is the ID of the image that was cleaned up.
	ImageID ImageID
	// ImageName is the name/tag of the image that was cleaned up.
	ImageName string
	// ContainerName is the name of the container that was using this image before the update.
	ContainerName string
}
