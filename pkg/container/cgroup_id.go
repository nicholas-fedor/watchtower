package container

import (
	"fmt"
	"iter"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// minMatchGroups is the minimum number of regex match groups expected.
// - 1 for the full match, 1 for the captured ID (total of 2).
const minMatchGroups = 2

// dockerContainerPattern matches Docker container IDs in cgroup data.
// It captures a 64-character hexadecimal ID after "/docker/" (e.g., "11:perf_event:/docker/abc...def").
var dockerContainerPattern = regexp.MustCompile(`[0-9]+:.*:/docker/([a-f0-9]{64})`)

// readFileFunc allows mocking file reading in tests; defaults to os.ReadFile.
var readFileFunc = os.ReadFile

// GetRunningContainerID retrieves the current container ID from the process's cgroup information.
//
// It reads the cgroup file (/proc/<pid>/cgroup) and extracts the Docker container ID.
//
// Returns:
//   - types.ContainerID: 64-character hexadecimal container ID if successful.
//   - error: Non-nil if file reading or ID extraction fails, nil on success.
func GetRunningContainerID() (types.ContainerID, error) {
	// Construct the path to the cgroup file using the current process ID (PID)
	filePath := fmt.Sprintf("/proc/%d/cgroup", os.Getpid())

	// Read the cgroup file content.
	file, err := readFileFunc(filePath)
	if err != nil {
		logrus.WithError(err).WithField("file", filePath).Debug("Failed to read cgroup file")

		return "", fmt.Errorf("%w: %w", errReadCgroupFile, err)
	}

	logrus.WithField("file", filePath).Debug("Read cgroup file successfully")

	// Extract the container ID from the file content.
	containerID, err := getRunningContainerIDFromString(string(file))
	if err != nil {
		logrus.WithError(err).
			WithField("file", filePath).
			Debug("Failed to extract container ID from cgroup")

		return "", fmt.Errorf("%w: %w", errExtractContainerID, err)
	}

	return containerID, nil
}

// getRunningContainerIDFromString extracts a container ID from a cgroup string.
//
// It uses regex to find a 64-character hexadecimal ID after "/docker/" in single-line or multiline input.
//
// Parameters:
//   - cgroupString: Cgroup data string to parse.
//
// Returns:
//   - types.ContainerID: Extracted container ID if found.
//   - error: Non-nil if no valid ID is found, nil on success.
func getRunningContainerIDFromString(cgroupString string) (types.ContainerID, error) {
	// Choose iteration method based on input format.
	var lines iter.Seq[string]
	if strings.Contains(cgroupString, "\n") {
		// For multiline input (e.g., full /proc/<pid>/cgroup content), use strings.Lines to iterate over each line
		lines = strings.Lines(cgroupString)
	} else {
		// For single-line input, create a simple iterator that yields just the input string
		lines = func(yield func(string) bool) {
			yield(cgroupString)
		}
	}

	// Iterate over all lines (single or multiple) to find a matching container ID
	for line := range lines {
		// Remove trailing newline for consistent matching, as /proc/<pid>/cgroup lines end with \n
		trimmedLine := strings.TrimRight(line, "\n")

		// Apply the regex to find a Docker container ID in the line
		matches := dockerContainerPattern.FindStringSubmatch(trimmedLine)

		// Log debug information about the line being processed
		logrus.WithFields(logrus.Fields{
			"line":    trimmedLine,
			"matches": matches,
		}).Debug("Processed cgroup line for container ID")

		// Verify a match with the expected groups (full match + ID).
		if len(matches) >= minMatchGroups {
			// The captured group (matches[1]) is the 64-character ID; regex ensures length and hex format
			id := types.ContainerID(matches[1])
			logrus.WithField("id", id).Debug("Extracted container ID from cgroup")

			return id, nil
		}
	}

	// Return an error if no ID is found after processing all lines.
	return "", fmt.Errorf("%w: %q", errNoValidContainerID, cgroupString)
}
