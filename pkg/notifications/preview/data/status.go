package data

import "github.com/nicholas-fedor/watchtower/pkg/types"

//nolint:errname // containerStatus is not an error type, it contains an error field
type containerStatus struct {
	containerID    types.ContainerID
	oldImage       types.ImageID
	newImage       types.ImageID
	containerName  string
	imageName      string
	containerError error
	state          State
	monitorOnly    bool
	newContainerID types.ContainerID
}

func (u *containerStatus) ID() types.ContainerID {
	return u.containerID
}

func (u *containerStatus) Name() string {
	return u.containerName
}

func (u *containerStatus) CurrentImageID() types.ImageID {
	return u.oldImage
}

func (u *containerStatus) LatestImageID() types.ImageID {
	return u.newImage
}

func (u *containerStatus) ImageName() string {
	return u.imageName
}

func (u *containerStatus) Error() string {
	if u.containerError == nil {
		return ""
	}

	return u.containerError.Error()
}

func (u *containerStatus) State() string {
	return string(u.state)
}

func (u *containerStatus) IsMonitorOnly() bool {
	return u.monitorOnly
}

func (u *containerStatus) NewContainerID() types.ContainerID {
	return u.newContainerID
}
