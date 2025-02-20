package mocks

import (
	"fmt"
	"os"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

type imageRef struct {
	id   types.ImageID
	file string
}

func (ir *imageRef) getFileName() string {
	return fmt.Sprintf("./mocks/data/image_%v.json", ir.file)
}

type ContainerRef struct {
	name       string
	id         types.ContainerID
	image      *imageRef
	file       string
	references []*ContainerRef
	isMissing  bool
}

func (cr *ContainerRef) getContainerFile() (containerFile string, err error) {
	file := cr.file
	if file == "" {
		file = cr.name
	}

	containerFile = fmt.Sprintf("./mocks/data/container_%v.json", file)
	_, err = os.Stat(containerFile)

	return containerFile, err
}

func (cr *ContainerRef) ContainerID() types.ContainerID {
	return cr.id
}
