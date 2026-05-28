package container

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"

	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// resolveRegistryMirrorConfig fetches the registry mirror configuration from the Docker daemon.
//
// It calls Info() and returns the system.Info containing both global mirrors
// (RegistryConfig.Mirrors) and per-registry mirrors (RegistryConfig.IndexConfigs).
// Returns nil if the call fails or no mirrors are configured.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - *dockerSystem.Info: System info with mirror configuration, or nil if unavailable.
func (c imageClient) resolveRegistryMirrorConfig(ctx context.Context) *dockerSystem.Info {
	info, err := c.api.Info(
		ctx,
		dockerClient.InfoOptions{},
	)
	if err != nil {
		logrus.WithError(err).
			Debug("Failed to get system info for registry mirror resolution")

		return nil
	}

	if info.Info.RegistryConfig == nil {
		logrus.Debug("No registry mirror configuration in Docker daemon")

		return nil
	}

	hasGlobal := len(info.Info.RegistryConfig.Mirrors) > 0
	hasPerRegistry := len(info.Info.RegistryConfig.IndexConfigs) > 0

	if !hasGlobal && !hasPerRegistry {
		logrus.Debug("No registry mirrors configured in Docker daemon")

		return nil
	}

	logrus.WithFields(logrus.Fields{
		"global_mirrors":     info.Info.RegistryConfig.Mirrors,
		"per_registry_count": len(info.Info.RegistryConfig.IndexConfigs),
	}).Debug("Resolved registry mirror configuration from Docker daemon")

	return &info.Info
}

// buildMirrorEndpoints returns the list of registry endpoints to try for digest comparison.
//
// It checks per-registry mirrors first (from IndexConfigs), then falls back to global mirrors.
// An empty string in the returned list means use the canonical registry host. The canonical
// host is always appended as the final fallback.
//
// Mirrors are resolved for all registries, not just Docker Hub.
// If no mirrors are configured for the image's registry, nil is returned (use canonical behavior).
//
// Parameters:
//   - sourceContainer: The container whose image is being checked.
//   - info: System info with mirror configuration from the Docker daemon (may be nil).
//
// Returns:
//   - []string: List of host overrides to try. Empty string means use canonical host.
func (c imageClient) buildMirrorEndpoints(
	sourceContainer types.Container,
	info *dockerSystem.Info,
) []string {
	if info == nil || info.RegistryConfig == nil {
		return nil
	}

	registryHost, err := auth.GetRegistryAddress(sourceContainer.ImageName())
	if err != nil {
		return nil
	}

	var mirrors []string

	// Check per-registry mirrors for the canonical host first.
	if idx, ok := info.RegistryConfig.IndexConfigs[registryHost]; ok {
		mirrors = append(mirrors, idx.Mirrors...)
	}

	// For Docker Hub images, fall back to the docker.io domain key
	// (the canonical host is index.docker.io but IndexConfigs uses docker.io).
	if len(mirrors) == 0 && registryHost == auth.DockerRegistryHost {
		if idx, ok := info.RegistryConfig.IndexConfigs[auth.DockerRegistryDomain]; ok {
			mirrors = append(mirrors, idx.Mirrors...)
		}
	}

	// Fall back to global mirrors if no per-registry mirrors found.
	if len(mirrors) == 0 {
		mirrors = info.RegistryConfig.Mirrors
	}

	if len(mirrors) == 0 {
		return nil
	}

	endpoints := make([]string, 0, len(mirrors)+1)

	for _, mirror := range mirrors {
		mirror = strings.TrimSpace(mirror)
		if mirror != "" {
			endpoints = append(endpoints, mirror)
		}
	}

	// Always append canonical host as final fallback.
	endpoints = append(endpoints, "")

	return endpoints
}
