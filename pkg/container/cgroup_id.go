package container

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// dockerContainerPattern matches Docker container IDs in cgroup data.
// The pattern captures a 64-character hexadecimal ID after "/docker/".
// - [0-9]+: matches one or more digits followed by a colon (e.g., "11:")
// - .*: matches any characters (greedy) followed by a colon (e.g., "perf_event:")
// - /docker/ matches the literal string "/docker/"
// - ([a-f0-9]{64}) captures exactly 64 lowercase hexadecimal characters as the container ID
var dockerContainerPattern = regexp.MustCompile(`[0-9]+:.*:/docker/([a-f0-9]{64})`)

// minMatchGroups is the minimum number of regex match groups expected.
// - 1 for the full match, 1 for the captured ID (total of 2).
const minMatchGroups = 2

// readFileFunc is a variable to allow mocking file reading in tests.
// Defaults to os.ReadFile but can be overridden for testing purposes.
var readFileFunc = os.ReadFile

// Static error definitions.
var (
	ErrNoValidContainerID = errors.New("no valid docker container ID found in input")
	ErrReadCgroupFile     = errors.New("failed to read cgroup file")
	ErrExtractContainerID = errors.New("failed to extract container ID")
)

// GetRunningContainerID retrieves the current container ID from the process's cgroup information.
// It reads the cgroup file (/proc/<pid>/cgroup) for the current process and extracts the ID.
// Returns an error if the file cannot be read or no valid ID is found.
// The returned ID is a 64-character hexadecimal string unique to the Docker container.
func GetRunningContainerID() (types.ContainerID, error) {
	// Construct the path to the cgroup file using the current process ID (PID)
	file, err := readFileFunc(fmt.Sprintf("/proc/%d/cgroup", os.Getpid()))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrReadCgroupFile, err)
	}

	// Pass the file content to the extraction function and handle any errors
	id, err := getRunningContainerIDFromString(string(file))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrExtractContainerID, err)
	}

	return id, nil
}

// getRunningContainerIDFromString extracts a container ID from a cgroup string.
// It processes the input string, which may be single-line or multiline, to find a 64-character
// hexadecimal ID following "/docker/". Returns the ID and nil on success, or an empty string
// and an error if no valid ID is found. Uses regex matching for precision and logs debug info.
func getRunningContainerIDFromString(cgroupString string) (types.ContainerID, error) {
	// Define an iterator for lines; behavior depends on whether the input is single-line or multiline
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
			"pattern": dockerContainerPattern.String(),
			"matches": matches,
		}).Debug("Processing input line")
		// Check if the regex found a match with at least the full match and the captured ID
		if len(matches) >= minMatchGroups {
			// The captured group (matches[1]) is the 64-character ID; regex ensures length and hex format
			return types.ContainerID(matches[1]), nil
		}
	}
	// If no valid ID is found after checking all lines, return an error with the input for context
	return "", fmt.Errorf("%w: %q", ErrNoValidContainerID, cgroupString)
}
