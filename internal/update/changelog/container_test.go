package changelog

import (
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// testContainer returns a minimal container reference for probe tests.
func testContainer() *container.Container {
	return modelfixture.ContainerWithoutImageInfo(containerfixture.WithImageName("org/app:latest"))
}

// pinnedContainer returns a container whose image is pinned to a digest.
func pinnedContainer() *container.Container {
	return modelfixture.ContainerWithoutImageInfo(containerfixture.WithImageName("org/app@sha256:abc"))
}
