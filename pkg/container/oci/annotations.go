package oci

import (
	"strings"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Annotations are OCI image labels from inspect. They never associate a Git watcher.
type Annotations struct {
	Source        string
	URL           string
	Documentation string
	Revision      string
	Version       string
}

// Read returns OCI annotations from the container's image config labels.
//
// Parameters:
//   - c: Container whose image inspect labels are read.
//
// Returns:
//   - Annotations: Empty fields when image info is missing.
func Read(c types.Container) Annotations {
	if c == nil || !c.HasImageInfo() {
		return Annotations{}
	}

	info := c.ImageInfo()
	if info == nil || info.Config == nil || info.Config.Labels == nil {
		return Annotations{}
	}

	// Image inspect stores OCI annotations as ordinary config labels.
	labels := info.Config.Labels

	return Annotations{
		Source:        strings.TrimSpace(labels[SourceLabel]),
		URL:           strings.TrimSpace(labels[URLLabel]),
		Documentation: strings.TrimSpace(labels[DocumentationLabel]),
		Revision:      strings.TrimSpace(labels[RevisionLabel]),
		Version:       strings.TrimSpace(labels[VersionLabel]),
	}
}
