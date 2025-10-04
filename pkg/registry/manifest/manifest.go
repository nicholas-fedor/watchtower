// Package manifest provides functionality for constructing URLs to access container
// image manifests in Watchtower. It handles parsing image references and building
// registry-specific manifest URLs for digest retrieval and other operations.
package manifest

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for manifest operations.
var (
	// errMissingTag indicates the parsed image reference lacks a tag.
	errMissingTag = errors.New("parsed container image reference has no tag")
	// errFailedParseImageName indicates a failure to parse the container’s image name.
	errFailedParseImageName = errors.New("failed to parse image name")
)

// BuildManifestURL constructs a URL for accessing a container’s image manifest from its registry.
//
// It parses the image name into a normalized reference, extracts the registry host, and builds a URL with path and tag,
// using the provided scheme.
//
// Parameters:
//   - container: Container with image info for URL construction.
//   - scheme: The scheme to use for the URL (e.g., "https" or "http").
//
// Returns:
//   - string: Manifest URL (e.g., "https://index.docker.io/v2/library/alpine/manifests/latest") if successful.
//   - error: Non-nil if parsing or tagging fails, nil on success.
func BuildManifestURL(container types.Container, scheme string) (string, error) {
	// Set up logging fields for consistent tracking.
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Parse the image name into a normalized reference for reliable processing.
	normalizedRef, err := reference.ParseDockerRef(container.ImageName())
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to parse image name")

		return "", fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	// Ensure the reference includes a tag by casting to NamedTagged.
	normalizedTaggedRef, isTagged := normalizedRef.(reference.NamedTagged)
	if !isTagged {
		logrus.WithFields(fields).
			WithField("ref", normalizedRef.String()).
			Debug("Missing tag in image reference")

		return "", fmt.Errorf("%w: %s", errMissingTag, normalizedRef.String())
	}

	// Extract the registry host and image components.
	host, _ := auth.GetRegistryAddress(normalizedTaggedRef.Name())
	img, tag := reference.Path(normalizedTaggedRef), normalizedTaggedRef.Tag()

	// Use the provided scheme.

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"host":       host,
		"image_path": img,
		"tag":        tag,
		"scheme":     scheme,
	}).Debug("Constructed manifest URL components")

	// Build the manifest URL with scheme, host, and path.
	url := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", img, tag),
	}
	urlStr := url.String()

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"url": urlStr,
	}).Debug("Built manifest URL")

	return urlStr, nil
}
