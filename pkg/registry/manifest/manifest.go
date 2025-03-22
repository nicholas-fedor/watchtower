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

var errMissingTag = errors.New("parsed container image reference has no tag")

// BuildManifestURL constructs a URL for accessing a container’s image manifest from its registry.
// It parses the container’s image name into a normalized reference, extracts the registry host,
// and builds a URL with the appropriate path and tag for manifest retrieval.
func BuildManifestURL(container types.Container) (string, error) {
	normalizedRef, err := reference.ParseDockerRef(container.ImageName())
	if err != nil {
		return "", fmt.Errorf("failed to parse image name: %w", err)
	}

	normalizedTaggedRef, isTagged := normalizedRef.(reference.NamedTagged)
	if !isTagged {
		return "", fmt.Errorf("%w: %s", errMissingTag, normalizedRef.String())
	}

	host, _ := helpers.GetRegistryAddress(normalizedTaggedRef.Name())
	img, tag := reference.Path(normalizedTaggedRef), normalizedTaggedRef.Tag()

	logrus.WithFields(logrus.Fields{
		"image":      img,
		"tag":        tag,
		"normalized": normalizedTaggedRef.Name(),
		"host":       host,
	}).Debug("Parsing image ref")

	url := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", img, tag),
	}

	return url.String(), nil
}
