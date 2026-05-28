package container

import (
	"context"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"

	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"
)

// resolveRegistryMirrorConfig fetches the registry mirror configuration from the Docker daemon.
//
// It calls Info() and returns the system.Info containing global mirrors (RegistryConfig.Mirrors).
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

	if len(info.Info.RegistryConfig.Mirrors) == 0 {
		logrus.Debug("No registry mirrors configured in Docker daemon")

		return nil
	}

	sanitized := make([]string, 0, len(info.Info.RegistryConfig.Mirrors))
	for _, m := range info.Info.RegistryConfig.Mirrors {
		u, err := url.Parse(m)
		if err == nil && u.Host != "" {
			sanitized = append(sanitized, u.Host)
		} else {
			sanitized = append(sanitized, "<redacted>")
		}
	}

	logrus.WithFields(logrus.Fields{
		"global_mirrors": sanitized,
	}).Debug("Resolved registry mirror configuration from Docker daemon")

	return &info.Info
}

// buildMirrorEndpoints returns the list of registry endpoints to try for digest comparison.
//
// It uses the global mirrors from the Docker daemon configuration.
// An empty string in the returned list means use the canonical registry host.
// The canonical host is always appended as the final fallback.
//
// If no mirrors are configured, nil is returned (use canonical behavior).
//
// Parameters:
//   - info: System info with mirror configuration from the Docker daemon (may be nil).
//
// Returns:
//   - []string: List of host overrides to try. Empty string means use canonical host.
func (c imageClient) buildMirrorEndpoints(
	info *dockerSystem.Info,
) []string {
	if info == nil || info.RegistryConfig == nil {
		return nil
	}

	mirrors := info.RegistryConfig.Mirrors
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
