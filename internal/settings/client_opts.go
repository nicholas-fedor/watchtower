package settings

import (
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
)

// ClientOptions builds container.ClientOptions from the resolved Config.
//
// Parameters:
//   - none (receiver Config).
//
// Returns:
//   - container.ClientOptions: Options for container.NewClient.
func (c Config) ClientOptions() containerClient.ClientOptions {
	return containerClient.ClientOptions{
		IncludeStopped:          c.Client.IncludeStopped,
		IncludeRestarting:       c.Client.IncludeRestarting,
		ReviveStopped:           c.Client.ReviveStopped,
		RemoveVolumes:           c.Client.RemoveVolumes,
		DisableMemorySwappiness: c.Compatibility.DisableMemorySwappiness,
		CPUCopyMode:             c.Compatibility.CPUCopyMode,
		WarnOnHeadFailed:        containerImage.WarningStrategy(c.Client.WarnOnHeadFailure),
		Registry:                c.Registry,
	}
}
