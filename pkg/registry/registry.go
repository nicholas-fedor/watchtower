package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/registry/hosts"
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
func GetPullOptions(log *zerolog.Logger, imageName string) (dockerClient.ImagePullOptions, error) {
	// Set up logging fields for consistent tracking.
	fields := map[string]any{
		"image": imageName,
	}

	log.Debug().
		Fields(fields).
		Msg("Retrieving pull options")

	// Fetch encoded registry credentials for the image.
	registryCredentials, err := EncodedAuth(log, imageName)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to get authentication credentials")

		return dockerClient.ImagePullOptions{}, fmt.Errorf("%w: %w", errFailedGetAuth, err)
	}

	// Return empty options if no auth is available.
	if registryCredentials == "" {
		log.Debug().
			Fields(fields).
			Msg("No authentication credentials retrieved")

		return dockerClient.ImagePullOptions{}, nil
	}

	// Log non-sensitive context only in trace mode.
	// Never log credential payload.
	if log.GetLevel() == zerolog.TraceLevel {
		log.Trace().
			Fields(fields).
			Bool("has_credentials", true).
			Msg("Retrieved authentication credentials")
	}

	// Configure pull options with auth and a default privilege handler.
	pullOptions := dockerClient.ImagePullOptions{
		RegistryAuth: registryCredentials,
		PrivilegeFunc: func(ctx context.Context) (string, error) {
			return DefaultAuthHandler(log, ctx)
		},
	}

	log.Debug().
		Fields(fields).
		Msg("Configured pull options")

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
func DefaultAuthHandler(log *zerolog.Logger, _ context.Context) (string, error) {
	// Log the auth rejection and proceed without credentials.
	log.Debug().Msg("Authentication rejected, retrying without credentials")

	return "", nil
}

// WarnOnAPIConsumption determines whether to warn about API consumption for a container's registry.
//
// It returns true for registries supporting HEAD requests (e.g., Docker Hub, GHCR) or if parsing fails.
//
// Parameters:
//   - container: Container with image info for registry check.
//
// Returns:
//   - bool: True if a warning is warranted, false otherwise.
func WarnOnAPIConsumption(log *zerolog.Logger, container types.Container) bool {
	// Set up logging fields for tracking.
	fields := map[string]any{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Parse the image name into a normalized reference.
	normalizedRef, err := reference.ParseNormalizedNamed(container.ImageName())
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to parse image reference, assuming API consumption")

		return true
	}

	// Extract the registry host from the reference.
	containerHost, err := auth.GetRegistryAddress(log, normalizedRef.Name())
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to get registry address, assuming API consumption")

		return true
	}

	// Check if the registry is known to support HEAD requests.
	if containerHost == hosts.DockerRegistryHost || containerHost == hosts.GitHubRegistryDomain {
		log.Debug().
			Fields(fields).
			Str("host", containerHost).
			Msg("Registry supports HEAD requests, warning on API consumption")

		return true
	}

	// No warning if registry behavior is unknown.
	log.Debug().
		Fields(fields).
		Str("host", containerHost).
		Msg("Registry behavior unknown, no API consumption warning")

	return false
}
