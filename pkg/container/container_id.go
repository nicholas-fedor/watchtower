package container

import (
	"context"
	"fmt"
	"iter"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for container ID detection.
const (
	minMatchGroups     = 2
	minMountinfoParts  = 2
	minMountinfoFields = 4
)

// Regex patterns for container ID extraction.
var (
	dockerContainerPattern = regexp.MustCompile(`[0-9]+:.*:/docker/([a-f0-9]{64})`)
	containerIDPattern     = regexp.MustCompile(`/containers/([a-f0-9]{64})`)
)

// File reading functions for testing mocks.
var (
	ReadMountinfoFunc = os.ReadFile
	ReadCgroupFunc    = os.ReadFile
)

// GetCurrentContainerID retrieves the current container ID using a fallback strategy.
// It attempts multiple detection methods in order of preference and reliability.
//
// The detection methods are tried in the following order:
// 1. Mountinfo-based detection - cgroup v2 compatible
// 2. Cgroup file parsing - cgroup v1 compatible
// 3. Hostname matching - fallback using Docker API
//
// Parameters:
//   - client: Docker client interface for container operations
//
// Returns:
//   - types.ContainerID: The detected container ID if successful
//   - error: Non-nil if all detection methods fail, containing the last error encountered
func GetCurrentContainerID(ctx context.Context, client Client) (types.ContainerID, error) {
	// Collect errors from failed detection attempts for final error reporting
	var errs []error

	// First attempt: Mountinfo-based detection - most reliable when available
	logrus.Debug("Attempting to get current container ID using mountinfo detection")

	containerID, err := GetContainerIDFromMountinfo()
	if err == nil {
		logrus.WithField("container_id", containerID).
			Debug("Successfully detected container ID using mountinfo")

		return containerID, nil
	}

	logrus.WithError(err).Debug("Mountinfo detection failed")
	errs = append(errs, err)

	// Second attempt: Cgroup file parsing - works in most containerized environments
	logrus.Debug("Attempting to get current container ID using cgroup file parsing")

	containerID, err = GetContainerIDFromCgroupFile()
	if err == nil {
		logrus.WithField("container_id", containerID).
			Debug("Successfully detected container ID using cgroup file")

		return containerID, nil
	}

	logrus.WithError(err).Debug("Cgroup file parsing failed")
	errs = append(errs, err)

	// Third attempt: Hostname matching - fallback using Docker API
	logrus.Debug("Attempting to get current container ID using hostname matching")

	containerID, err = GetContainerIDFromHostname(ctx, client)
	if err == nil {
		logrus.WithField("container_id", containerID).
			Debug("Successfully detected container ID using hostname matching")

		return containerID, nil
	}

	logrus.WithError(err).Debug("Hostname matching failed")
	errs = append(errs, err)

	// All methods failed - return the last error with context
	lastErr := errs[len(errs)-1]
	logrus.WithError(lastErr).Error("All container ID detection methods failed")

	return "", fmt.Errorf("failed to detect current container ID: %w", lastErr)
}

// GetContainerIDFromMountinfo retrieves the container ID from /proc/self/mountinfo.
// Uses the mountinfo file to find container paths containing /containers/<id>.
func GetContainerIDFromMountinfo() (types.ContainerID, error) {
	file, err := ReadMountinfoFunc("/proc/self/mountinfo")
	if err != nil {
		logrus.WithError(err).
			WithField("file", "/proc/self/mountinfo").
			Debug("Failed to read mountinfo file")

		return "", errReadMountinfoFile
	}

	logrus.WithField("file", "/proc/self/mountinfo").Debug("Read mountinfo file successfully")

	containerID, err := ParseContainerIDFromMountinfo(string(file))
	if err != nil {
		logrus.WithError(err).
			WithField("file", "/proc/self/mountinfo").
			Debug("Failed to extract container ID from mountinfo")

		return "", errExtractContainerIDFromMountinfo
	}

	return containerID, nil
}

// ParseContainerIDFromMountinfo parses the mountinfo string to extract container ID.
func ParseContainerIDFromMountinfo(mountinfoString string) (types.ContainerID, error) {
	lines := strings.SplitSeq(strings.TrimSpace(mountinfoString), "\n")

	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, " - ")
		if len(parts) < minMountinfoParts {
			continue
		}

		firstPart := parts[0]

		fields := strings.Split(firstPart, " ")
		if len(fields) >= minMountinfoFields {
			root := fields[3]
			logrus.WithField("root", root).Debug("Processing mountinfo root")

			if id := ExtractContainerIDFromPath(root); id != "" {
				logrus.WithField("id", id).Debug("Extracted container ID from mountinfo root")

				return id, nil
			}
		}
	}

	return "", errNoValidContainerID
}

// ExtractContainerIDFromPath extracts container ID from a path containing /containers/<id>.
func ExtractContainerIDFromPath(path string) types.ContainerID {
	matches := containerIDPattern.FindStringSubmatch(path)
	if len(matches) >= minMatchGroups {
		return types.ContainerID(matches[1])
	}

	return ""
}

// GetContainerIDFromCgroupFile retrieves the container ID from /proc/<pid>/cgroup.
// Uses the cgroup file to find Docker container paths.
func GetContainerIDFromCgroupFile() (types.ContainerID, error) {
	filePath := fmt.Sprintf("/proc/%d/cgroup", os.Getpid())

	file, err := ReadCgroupFunc(filePath)
	if err != nil {
		logrus.WithError(err).WithField("file", filePath).Debug("Failed to read cgroup file")

		return "", errReadCgroupFile
	}

	logrus.WithField("file", filePath).Debug("Read cgroup file successfully")

	containerID, err := ParseContainerIDFromCgroupString(string(file))
	if err != nil {
		logrus.WithError(err).
			WithField("file", filePath).
			Debug("Failed to extract container ID from cgroup")

		return "", errExtractContainerID
	}

	return containerID, nil
}

// ParseContainerIDFromCgroupString parses the cgroup string to extract container ID.
func ParseContainerIDFromCgroupString(cgroupString string) (types.ContainerID, error) {
	var lines iter.Seq[string]
	if strings.Contains(cgroupString, "\n") {
		lines = strings.Lines(cgroupString)
	} else {
		lines = func(yield func(string) bool) {
			yield(cgroupString)
		}
	}

	for line := range lines {
		trimmedLine := strings.TrimRight(line, "\n")

		matches := dockerContainerPattern.FindStringSubmatch(trimmedLine)

		logrus.WithFields(logrus.Fields{
			"line":    trimmedLine,
			"matches": matches,
			"pattern": dockerContainerPattern.String(),
		}).Debug("Processed cgroup line for container ID")

		if len(matches) >= minMatchGroups {
			id := types.ContainerID(matches[1])
			logrus.WithField("id", id).Debug("Extracted container ID from cgroup")

			return id, nil
		}
	}

	return "", fmt.Errorf("%w: %q", errNoValidContainerID, cgroupString)
}

// GetContainerIDFromHostname retrieves the container ID by matching the HOSTNAME env var.
// Uses Docker API to list containers and find matching hostname.
func GetContainerIDFromHostname(ctx context.Context, client Client) (types.ContainerID, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		return "", ErrContainerIDNotFound
	}

	containers, err := client.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list all containers: %w", err)
	}

	for _, c := range containers {
		containerInfo := c.ContainerInfo()
		if containerInfo == nil {
			logrus.Debug("Container info is nil, skipping hostname check")

			continue
		}

		if containerInfo.Config == nil {
			logrus.Debug("Container config is nil, skipping hostname check")

			continue
		}

		if containerInfo.Config.Hostname == hostname {
			return c.ID(), nil
		}
	}

	return "", errNoContainerWithHostname
}
