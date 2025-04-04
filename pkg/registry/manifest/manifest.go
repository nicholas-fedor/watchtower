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

	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
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
// It parses the container’s image name into a normalized reference, extracts the registry host,
// and builds a URL with the appropriate path and tag for manifest retrieval.
func BuildManifestURL(container types.Container) (string, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	normalizedRef, err := reference.ParseDockerRef(container.ImageName())
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to parse image name")

		return "", fmt.Errorf("%w: %w", errFailedParseImageName, err)
	}

	normalizedTaggedRef, isTagged := normalizedRef.(reference.NamedTagged)
	if !isTagged {
		logrus.WithFields(fields).
			WithField("ref", normalizedRef.String()).
			Debug("Missing tag in image reference")

		return "", fmt.Errorf("%w: %s", errMissingTag, normalizedRef.String())
	}

	host, _ := helpers.GetRegistryAddress(normalizedTaggedRef.Name())
	img, tag := reference.Path(normalizedTaggedRef), normalizedTaggedRef.Tag()

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"host":       host,
		"image_path": img,
		"tag":        tag,
	}).Debug("Constructed manifest URL components")

	url := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", img, tag),
	}
	urlStr := url.String()

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"url": urlStr,
	}).Debug("Built manifest URL")

	return urlStr, nil
}
