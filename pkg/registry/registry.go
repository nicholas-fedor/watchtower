package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	dockerImage "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for registry operations.
var (
	// errFailedGetAuth indicates a failure to retrieve authentication credentials for an image.
	errFailedGetAuth = errors.New("failed to get authentication credentials")
)

// GetPullOptions creates a struct with all options needed for pulling images from a registry.
//
// It retrieves encoded authentication credentials and configures pull options with a privilege function.
//
// Parameters:
//   - imageName: Name of the image to pull (e.g., "docker.io/library/alpine").
//
// Returns:
//   - image.PullOptions: Configured pull options if successful.
//   - error: Non-nil if auth retrieval fails, nil on success.
func GetPullOptions(imageName string) (dockerImage.PullOptions, error) {
	// Set up logging fields for consistent tracking.
	fields := logrus.Fields{
		"image": imageName,
	}

	logrus.WithFields(fields).Debug("Retrieving pull options")

	// Fetch encoded registry credentials for the image.
	registryCredentials, err := EncodedAuth(imageName)
	if err != nil {
		logrus.WithError(err).WithFields(fields).Debug("Failed to get authentication credentials")

		return dockerImage.PullOptions{}, fmt.Errorf("%w: %w", errFailedGetAuth, err)
	}

	// Return empty options if no auth is available.
	if registryCredentials == "" {
		logrus.WithFields(fields).Debug("No authentication credentials retrieved")

		return dockerImage.PullOptions{}, nil
	}

	// Log auth details only in trace mode to protect sensitive data.
	if logrus.GetLevel() == logrus.TraceLevel {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"auth": registryCredentials,
		}).Trace("Retrieved authentication credentials")
	}

	// Configure pull options with auth and a default privilege handler.
	pullOptions := dockerImage.PullOptions{
		RegistryAuth:  registryCredentials,
		PrivilegeFunc: DefaultAuthHandler,
	}

	logrus.WithFields(fields).Debug("Configured pull options")

	return pullOptions, nil
}

// DefaultAuthHandler is a privilege function called when initial authentication fails.
//
// It retries the request without credentials, as reusing the same auth is unlikely to succeed.
//
// Parameters:
//   - ctx: Context for request lifecycle control (unused here).
//
// Returns:
//   - string: Empty string to indicate no new credentials.
//   - error: Always nil, as no further action is taken.
func DefaultAuthHandler(_ context.Context) (string, error) {
	// Log the auth rejection and proceed without credentials.
	logrus.Debug("Authentication rejected, retrying without credentials")

	return "", nil
}

// WarnOnAPIConsumption determines whether to warn about API consumption for a container’s registry.
//
// It returns true for registries supporting HEAD requests (e.g., Docker Hub, GHCR) or if parsing fails.
//
// Parameters:
//   - container: Container with image info for registry check.
//
// Returns:
//   - bool: True if a warning is warranted, false otherwise.
func WarnOnAPIConsumption(container types.Container) bool {
	// Set up logging fields for tracking.
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Parse the image name into a normalized reference.
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to parse image reference, assuming API consumption")

		return true
	}

	// Extract the registry host from the reference.
	containerHost, err := auth.GetRegistryAddress(normalizedRef.Name())
	if err != nil {
		logrus.WithError(err).
			WithFields(fields).
			Debug("Failed to get registry address, assuming API consumption")

		return true
	}

	// Check if the registry is known to support HEAD requests.
	if containerHost == auth.DockerRegistryHost || containerHost == "ghcr.io" {
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"host": containerHost,
		}).Debug("Registry supports HEAD requests, warning on API consumption")

		return true
	}

	// No warning if registry behavior is unknown.
	logrus.WithFields(fields).WithFields(logrus.Fields{
		"host": containerHost,
	}).Debug("Registry behavior unknown, no API consumption warning")

	return false
}
