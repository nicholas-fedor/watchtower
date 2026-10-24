package modelfixture

import (
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
)

// Container builds a Container from containerfixture inspect data, with updates
// applied in order.
//
// Parameters:
//   - updates: Changes to the container or image inspect data.
//
// Returns:
//   - *container.Container: Container built from the inspect data.
func Container(updates ...containerfixture.Update) *container.Container {
	containerInfo, imageInfo := containerfixture.Inspect(updates...)

	return container.NewContainer(nil, containerInfo, imageInfo)
}
