package data

import "github.com/nicholas-fedor/watchtower/pkg/types"

type containerStatus struct {
	containerID   types.ContainerID
	oldImage      types.ImageID
	newImage      types.ImageID
	containerName string
	imageName     string
	error
	state State
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
	if u.error == nil {
		return ""
	}
	return u.error.Error()
}

func (u *containerStatus) State() string {
	return string(u.state)
}
