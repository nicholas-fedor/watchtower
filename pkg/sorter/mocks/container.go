// Package mocks provides mock implementations for container interfaces used in testing.
package mocks

import (
	"strings"

	dockerContainerTypes "github.com/docker/docker/api/types/container"
	dockerImageTypes "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// SimpleContainer implements a minimal Container interface for benchmarking.
type SimpleContainer struct {
	ContainerName      string
	ContainerID        types.ContainerID
	ContainerLinks     []string
	ContainerInfoField *dockerContainerTypes.InspectResponse
}

func (c *SimpleContainer) Name() string {
	return c.ContainerName
}

func (c *SimpleContainer) ID() types.ContainerID {
	return c.ContainerID
}

func (c *SimpleContainer) Links() []string {
	return c.ContainerLinks
}

func (c *SimpleContainer) IsWatchtower() bool {
	return false
}

func (c *SimpleContainer) ContainerInfo() *dockerContainerTypes.InspectResponse {
	if c.ContainerInfoField != nil {
		return c.ContainerInfoField
	}
	return &dockerContainerTypes.InspectResponse{
		ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/" + c.ContainerName},
		Config:            &dockerContainerTypes.Config{Labels: map[string]string{}},
	}
}

// IsRunning returns true for mock containers.
func (c *SimpleContainer) IsRunning() bool { return true }

func (c *SimpleContainer) ImageID() types.ImageID {
	if c.ContainerInfoField != nil {
		image := string(c.ContainerInfoField.Image)
		if strings.HasPrefix(image, "sha256:") {
			return types.ImageID(image)
		} else {
			return types.ImageID("sha256:" + image)
		}
	}
	return types.ImageID("sha256:" + string(c.ContainerID))
}

func (c *SimpleContainer) SafeImageID() types.ImageID {
	if c.ContainerInfoField != nil {
		return types.ImageID(c.ContainerInfoField.Config.Image)
	}
	return c.ImageID()
}

func (c *SimpleContainer) ImageName() string {
	if c.ContainerInfoField != nil {
		return c.ContainerInfoField.Config.Image
	}
	return "test-image"
}

func (c *SimpleContainer) Enabled() (bool, bool)                   { return true, true }
func (c *SimpleContainer) IsMonitorOnly(_ types.UpdateParams) bool { return false }

func (c *SimpleContainer) Scope() (string, bool) { return "", false }
func (c *SimpleContainer) ToRestart() bool       { return false }

func (c *SimpleContainer) StopSignal() string {
	if c.ContainerInfoField != nil {
		return c.ContainerInfoField.Config.StopSignal
	}
	return "SIGTERM"
}
func (c *SimpleContainer) HasImageInfo() bool                                    { return false }
func (c *SimpleContainer) ImageInfo() *dockerImageTypes.InspectResponse          { return nil }
func (c *SimpleContainer) GetLifecyclePreCheckCommand() string                   { return "" }
func (c *SimpleContainer) GetLifecyclePostCheckCommand() string                  { return "" }
func (c *SimpleContainer) GetLifecyclePreUpdateCommand() string                  { return "" }
func (c *SimpleContainer) GetLifecyclePostUpdateCommand() string                 { return "" }
func (c *SimpleContainer) GetLifecycleUID() (int, bool)                          { return 0, false }
func (c *SimpleContainer) GetLifecycleGID() (int, bool)                          { return 0, false }
func (c *SimpleContainer) VerifyConfiguration() error                            { return nil }
func (c *SimpleContainer) SetStale(_ bool)                                       {}
func (c *SimpleContainer) IsStale() bool                                         { return false }
func (c *SimpleContainer) IsNoPull(_ types.UpdateParams) bool                    { return false }
func (c *SimpleContainer) SetLinkedToRestarting(_ bool)                          {}
func (c *SimpleContainer) IsLinkedToRestarting() bool                            { return false }
func (c *SimpleContainer) PreUpdateTimeout() int {
	if c.ContainerInfoField != nil {
		return 0
	}
	return 30
}
func (c *SimpleContainer) PostUpdateTimeout() int {
	if c.ContainerInfoField != nil {
		return 0
	}
	return 30
}
func (c *SimpleContainer) IsRestarting() bool                                    { return false }
func (c *SimpleContainer) GetCreateConfig() *dockerContainerTypes.Config         { return nil }
func (c *SimpleContainer) GetCreateHostConfig() *dockerContainerTypes.HostConfig { return nil }

