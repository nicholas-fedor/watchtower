package container

import (
	"slices"
	"strings"
)

// Bind is a HostConfig.Binds entry split into its parts.
type Bind struct {
	// Source is the host path or volume name.
	Source string
	// Destination is the path inside the container.
	Destination string
	// Options are the comma-separated mount options after the destination,
	// such as ro or z. Empty when the entry has none.
	Options string
}

// ParseBind splits a HostConfig.Binds entry in source:destination[:options]
// form.
//
// A source that starts with a Windows drive letter, such as C:\data, keeps
// its drive letter when a path destination follows it.
//
// Parameters:
//   - bind: HostConfig.Binds entry.
//
// Returns:
//   - Bind: The entry's parts.
//   - bool: True when the entry has a non-empty source and destination.
func ParseBind(bind string) (Bind, bool) {
	// The first colon separates the source, except after a drive letter.
	// Podman volume names can be a single character, so v:/data names volume
	// v. A one-letter source is a drive letter only when the colon after it
	// is followed by a path, as in C:\data:/data.
	for _, start := range []int{2, 0} {
		if start > 0 && (len(bind) < start || bind[1] != ':') {
			continue
		}

		sep := strings.IndexByte(bind[start:], ':')
		if sep == -1 {
			continue
		}

		source := bind[:start+sep]
		destination, options, _ := strings.Cut(bind[start+sep+1:], ":")

		if source == "" || destination == "" {
			continue
		}

		if start > 0 && !strings.HasPrefix(destination, "/") && !strings.HasPrefix(destination, `\`) {
			continue
		}

		return Bind{Source: source, Destination: destination, Options: options}, true
	}

	return Bind{}, false
}

// ReadOnly reports whether the bind's options mark it read-only.
//
// Returns:
//   - bool: True when the options include ro.
func (b Bind) ReadOnly() bool {
	return slices.Contains(strings.Split(b.Options, ","), "ro")
}
