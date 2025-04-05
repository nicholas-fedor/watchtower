package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/image"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/registry/helpers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for registry operations.
var (
	// errFailedGetAuth indicates a failure to retrieve authentication credentials for an image.
	errFailedGetAuth = errors.New("failed to get authentication credentials")
)

// GetPullOptions creates a struct with all options needed for pulling images from a registry.
// It retrieves encoded authentication credentials for the specified image and configures
// pull options, including a privilege function for handling authentication retries.
func GetPullOptions(imageName string) (image.PullOptions, error) {
	fields := logrus.Fields{
		"image": imageName,
	}

	logrus.WithFields(fields).Debug("Retrieving pull options")

	auth, err := EncodedAuth(imageName)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get authentication credentials")

		return image.PullOptions{}, fmt.Errorf("%w: %w", errFailedGetAuth, err)
	}

	if auth == "" {
		logrus.WithFields(fields).Debug("No authentication credentials found")

		return image.PullOptions{}, nil
	}

	// Log auth value only in trace mode to avoid leaking credentials
	if logrus.GetLevel() == logrus.TraceLevel {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"auth": auth,
		}).Trace("Retrieved authentication credentials")
	}

	pullOptions := image.PullOptions{
		RegistryAuth:  auth,
		PrivilegeFunc: DefaultAuthHandler,
	}

	logrus.WithFields(fields).Debug("Configured pull options")

	return pullOptions, nil
}

// DefaultAuthHandler is a privilege function called when initial authentication fails.
// It logs the rejection and returns an empty string to retry the request without authentication,
// as retrying with the same credentials used in AuthConfig is unlikely to succeed.
func DefaultAuthHandler(_ context.Context) (string, error) {
	logrus.Debug("Authentication rejected, retrying without credentials")

	return "", nil
}

// WarnOnAPIConsumption determines whether to warn about API consumption for a container’s registry.
// It returns true if the registry is known to support HTTP HEAD requests for digest checking
// (e.g., Docker Hub, GHCR) or if parsing the container hostname fails, indicating uncertainty.
// It returns false if the registry’s behavior is unknown, avoiding unnecessary warnings.
func WarnOnAPIConsumption(container types.Container) bool {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to parse image reference, assuming API consumption")

		return true
	}

	containerHost, err := helpers.GetRegistryAddress(normalizedRef.Name())
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to get registry address, assuming API consumption")

		return true
	}

	if containerHost == helpers.DefaultRegistryHost || containerHost == "ghcr.io" {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"host": containerHost,
		}).Debug("Registry supports HEAD requests, warning on API consumption")

		return true
	}

	logrus.WithFields(fields).WithFields(logrus.Fields{
		"host": containerHost,
	}).Debug("Registry behavior unknown, no API consumption warning")

	return false
}
