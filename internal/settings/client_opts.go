package settings

import (
	"github.com/nicholas-fedor/watchtower/pkg/container"
)

// ClientOptions builds container.ClientOptions from the resolved Config.
//
// Parameters:
//   - none (receiver Config).
//
// Returns:
//   - container.ClientOptions: Options for container.NewClient.
func (c Config) ClientOptions() container.ClientOptions {
	return container.ClientOptions{
		IncludeStopped:          c.Client.IncludeStopped,
		IncludeRestarting:       c.Client.IncludeRestarting,
		ReviveStopped:           c.Client.ReviveStopped,
		RemoveVolumes:           c.Client.RemoveVolumes,
		DisableMemorySwappiness: c.Compatibility.DisableMemorySwappiness,
		CPUCopyMode:             c.Compatibility.CPUCopyMode,
		WarnOnHeadFailed:        container.WarningStrategy(c.Client.WarnOnHeadFailure),
		Registry:                c.Registry,
	}
}
