package manifest

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/distribution/reference"
	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
)

// BuildManifestURL from raw image data
func BuildManifestURL(container types.Container) (string, error) {
	normalizedRef, err := reference.ParseDockerRef(container.ImageName())
	if err != nil {
		return "", err
	}
	normalizedTaggedRef, isTagged := normalizedRef.(reference.NamedTagged)
	if !isTagged {
		return "", errors.New("Parsed container image ref has no tag: " + normalizedRef.String())
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
