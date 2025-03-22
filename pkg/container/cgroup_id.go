package container

import (
	"fmt"
	"os"
	"regexp"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// dockerContainerPattern matches Docker container IDs in cgroup data.
// The pattern captures a 64-character hexadecimal ID after "/docker/".
var dockerContainerPattern = regexp.MustCompile(`[0-9]+:.*:/docker/([a-f0-9]{64})`)

// minMatchGroups is the minimum number of regex match groups expected.
// It represents the full match plus the captured container ID.
const minMatchGroups = 2

// GetRunningContainerID retrieves the current container ID from the process's cgroup information.
// It reads the cgroup file for the current process and extracts the ID, returning an error if the file cannot be read.
func GetRunningContainerID() (types.ContainerID, error) {
	file, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", os.Getpid()))
	if err != nil {
		return "", fmt.Errorf("failed to read cgroup file: %w", err)
	}

	return getRunningContainerIDFromString(string(file)), nil
}

// getRunningContainerIDFromString extracts a container ID from a cgroup string.
// It uses a regex to find the 64-character hexadecimal ID and returns an empty string if no valid ID is found.
func getRunningContainerIDFromString(s string) types.ContainerID {
	matches := dockerContainerPattern.FindStringSubmatch(s)
	if len(matches) < minMatchGroups {
		return ""
	}

	return types.ContainerID(matches[1])
}
