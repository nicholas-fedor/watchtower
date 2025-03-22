package registry

import (
	"context"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/image"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// GetPullOptions creates a struct with all options needed for pulling images from a registry.
// It retrieves encoded authentication credentials for the specified image and configures
// pull options, including a privilege function for handling authentication retries.
func GetPullOptions(imageName string) (image.PullOptions, error) {
	auth, err := EncodedAuth(imageName)
	logrus.Debugf("Got image name: %s", imageName)

	if err != nil {
		return image.PullOptions{}, err
	}

	if auth == "" {
		return image.PullOptions{}, nil
	}

	// CREDENTIAL: Uncomment to log docker config auth
	// log.Tracef("Got auth value: %s", auth)

	return image.PullOptions{
		RegistryAuth:  auth,
		PrivilegeFunc: DefaultAuthHandler,
	}, nil
}

// DefaultAuthHandler is a privilege function called when initial authentication fails.
// It logs the rejection and returns an empty string to retry the request without authentication,
// as retrying with the same credentials used in AuthConfig is unlikely to succeed.
func DefaultAuthHandler(context.Context) (string, error) {
	logrus.Debug("Authentication request was rejected. Trying again without authentication")

	return "", nil
}

// WarnOnAPIConsumption determines whether to warn about API consumption for a container’s registry.
// It returns true if the registry is known to support HTTP HEAD requests for digest checking
// (e.g., Docker Hub, GHCR) or if parsing the container hostname fails, indicating uncertainty.
// It returns false if the registry’s behavior is unknown, avoiding unnecessary warnings.
func WarnOnAPIConsumption(container types.Container) bool {
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		return true
	}

	containerHost, err := helpers.GetRegistryAddress(normalizedRef.Name())
	if err != nil {
		return true
	}

	if containerHost == helpers.DefaultRegistryHost || containerHost == "ghcr.io" {
		return true
	}

	return false
}
