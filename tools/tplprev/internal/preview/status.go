package preview

import "github.com/nicholas-fedor/tplprev/internal/report"

var _ report.ContainerReport = (*containerStatus)(nil)

//nolint:errname // containerStatus is not an error type. It contains an error field.
type containerStatus struct {
	containerID    report.ContainerID
	oldImage       report.ImageID
	newImage       report.ImageID
	containerName  string
	imageName      string
	containerError error
	state          State
	monitorOnly    bool
	newContainerID report.ContainerID
}

func (u *containerStatus) ID() report.ContainerID {
	return u.containerID
}

func (u *containerStatus) Name() string {
	return u.containerName
}

func (u *containerStatus) CurrentImageID() report.ImageID {
	return u.oldImage
}

func (u *containerStatus) LatestImageID() report.ImageID {
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

func (u *containerStatus) NewContainerID() report.ContainerID {
	return u.newContainerID
}
