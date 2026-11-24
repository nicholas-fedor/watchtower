package client

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// ContainerReader reads the containers that identify Watchtower's own
// container.
type ContainerReader interface {
	// GetCurrentWatchtowerContainer returns the container with the given ID,
	// without its image info.
	GetCurrentWatchtowerContainer(
		ctx context.Context,
		containerID container.ContainerID,
	) (*container.Container, error)
	// ListContainers returns the containers that pass every filter.
	ListContainers(ctx context.Context, filter ...filters.Filter) ([]*container.Container, error)
	// ListLabeledContainers returns the containers whose label is set to
	// "true", in every state but removing and dead.
	ListLabeledContainers(ctx context.Context, label string) ([]*container.Container, error)
}

// Constants for container ID detection.
const (
	minMatchGroups     = 2
	minMountinfoParts  = 2
	minMountinfoFields = 4
	// userHZ is the clock tick rate of process start times in /proc, fixed
	// at 100 per second for user space on Linux.
	userHZ = 100
	// statStartTimeIndex is the index of the start time among the fields
	// that follow the command name in /proc/<pid>/stat, the 22nd field of
	// the line.
	statStartTimeIndex = 19
	// startTimeTolerance is how far a container's recorded start may be from
	// the start of its first process. The boot time in /proc/stat has whole
	// seconds, so the two differ by up to about a second.
	startTimeTolerance = 2 * time.Second
)

// Regex patterns for container ID extraction.
var (
	// dockerContainerPattern matches a cgroup file line, in the form
	// hierarchy-ID:controllers:path, whose path holds Docker's /docker/<id>.
	dockerContainerPattern = regexp.MustCompile(`[0-9]+:.*:/docker/([a-f0-9]{64})`)
	// scopeContainerPattern matches a cgroup file line whose path ends in the
	// systemd scope of a container: docker-<id>.scope for Docker and
	// libpod-<id>.scope for Podman, as used with the systemd cgroup driver.
	// The scope may be followed by a nested cgroup.
	scopeContainerPattern = regexp.MustCompile(`[0-9]+:.*:.*/(?:docker|libpod)-([a-f0-9]{64})\.scope(?:/|$)`)
	// containerIDPattern matches the container directory in a mount root:
	// /containers/<id> for Docker and /overlay-containers/<id> for Podman.
	// The ID must end the directory name.
	containerIDPattern = regexp.MustCompile(`/(?:overlay-)?containers/([a-f0-9]{64})(?:/|$)`)
)

// containerMarkers are files that container runtimes create inside a container,
// as paths relative to the filesystem root.
var containerMarkers = []string{
	"run/.containerenv", // Podman
	".dockerenv",        // Docker
}

// Errors for container ID detection operations.
var (
	// errReadMountinfoFile indicates a failure to read the mountinfo file for the current process.
	errReadMountinfoFile = errors.New("failed to read mountinfo file")
	// errExtractContainerIDFromMountinfo indicates a failure to extract a container ID from mountinfo data.
	errExtractContainerIDFromMountinfo = errors.New("failed to extract container ID from mountinfo")
	// errReadCgroupFile indicates a failure to read the cgroup file for the current process.
	errReadCgroupFile = errors.New("failed to read cgroup file")
	// errExtractContainerID indicates a failure to extract a container ID from the cgroup data.
	errExtractContainerID = errors.New("failed to extract container ID")
	// errNoValidContainerID indicates no valid Docker container ID was found in the input data.
	errNoValidContainerID = errors.New("no valid docker container ID found in input")
	// errContainerIDNotFound indicates the container ID could not be found using hostname matching.
	errContainerIDNotFound = errors.New("HOSTNAME environment variable is not set")
	// errNoContainerWithHostname indicates no container was found with the matching hostname.
	errNoContainerWithHostname = errors.New("no container found with matching hostname")
	// errSeveralWatchtowersJoinNetwork indicates that more than one Watchtower
	// container joins the network namespace of the container mountinfo names,
	// so mountinfo cannot tell which one is this one.
	errSeveralWatchtowersJoinNetwork = errors.New("several Watchtower containers join the network container named by mountinfo")
	// errNoBootTime indicates that /proc/stat holds no boot time.
	errNoBootTime = errors.New("no boot time in /proc/stat")
	// errMalformedProcessStat indicates that /proc/1/stat has no start time.
	errMalformedProcessStat = errors.New("no start time in /proc/1/stat")
)

// GetCurrentContainerID retrieves the current container ID using a fallback strategy.
// It attempts multiple detection methods in order of preference and reliability.
//
// The detection methods are tried in the following order:
// 1. Mountinfo-based detection - cgroup v2 compatible
// 2. Cgroup file parsing - cgroup v1 compatible
// 3. Hostname matching - fallback using Docker API
//
// A container that joins another container's network namespace sees that
// container's hostname, hosts and resolv.conf files, so mountinfo names the
// network container. When that container is not a Watchtower container and
// exactly one running Watchtower container joins its network namespace, that
// Watchtower container is the one returned. When several do and none can be
// told apart, mountinfo counts as failed and only the cgroup file is tried,
// since they all report the network container's hostname.
//
// The container is returned too when mountinfo detection read it from the
// daemon, without its image info when mountinfo names it directly, so the
// caller does not need to inspect it again.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for the lookups through client.
//   - fsys: Filesystem rooted at "/" holding /proc, read by the mountinfo and
//     cgroup methods.
//   - client: Docker client interface for container operations
//
// Returns:
//   - container.ContainerID: The detected container ID if successful
//   - *container.Container: The detected container, or nil when detection did
//     not read it from the daemon.
//   - error: Non-nil if all detection methods fail, containing the last error encountered
func GetCurrentContainerID(
	log *zerolog.Logger,
	ctx context.Context,
	fsys fs.FS,
	client ContainerReader,
) (container.ContainerID, *container.Container, error) {
	// Collect errors from failed detection attempts for final error reporting
	var errs []error

	// First attempt: Mountinfo-based detection - most reliable when available
	log.Debug().Msg("Attempting to get current container ID using mountinfo detection")

	var detected *container.Container

	containerID, mountinfoErr := GetContainerIDFromMountinfo(log, fsys)
	if mountinfoErr == nil {
		containerID, detected, mountinfoErr = joiningWatchtower(log, ctx, fsys, client, containerID)
	}

	if mountinfoErr == nil {
		log.Debug().
			Str("container_id", string(containerID)).
			Msg("Successfully detected container ID using mountinfo")

		return containerID, detected, nil
	}

	log.Debug().
		Err(mountinfoErr).
		Msg("Mountinfo detection failed")
	errs = append(errs, mountinfoErr)

	// Second attempt: Cgroup file parsing - works in most containerized environments
	log.Debug().Msg("Attempting to get current container ID using cgroup file parsing")

	containerID, err := GetContainerIDFromCgroupFile(log, fsys)
	if err == nil {
		log.Debug().
			Str("container_id", string(containerID)).
			Msg("Successfully detected container ID using cgroup file")

		return containerID, nil, nil
	}

	log.Debug().
		Err(err).
		Msg("Cgroup file parsing failed")
	errs = append(errs, err)

	if errors.Is(mountinfoErr, errSeveralWatchtowersJoinNetwork) {
		return "", nil, fmt.Errorf("failed to detect current container ID: %w", mountinfoErr)
	}

	// Third attempt: Hostname matching - fallback using Docker API
	log.Debug().Msg("Attempting to get current container ID using hostname matching")

	containerID, err = GetContainerIDFromHostname(log, ctx, client)
	if err == nil {
		log.Debug().
			Str("container_id", string(containerID)).
			Msg("Successfully detected container ID using hostname matching")

		return containerID, nil, nil
	}

	log.Debug().
		Err(err).
		Msg("Hostname matching failed")
	errs = append(errs, err)

	// All methods failed - return the last error with context. A binary on
	// the host has no container ID, so callers decide whether this matters.
	lastErr := errs[len(errs)-1]
	log.Debug().
		Err(lastErr).
		Msg("All container ID detection methods failed")

	return "", nil, fmt.Errorf("failed to detect current container ID: %w", lastErr)
}

// joiningWatchtower returns the Watchtower container that joins the network
// namespace of the container mountinfo names, or that container itself.
//
// The container mountinfo names is returned in these cases:
//   - It is a Watchtower container.
//   - It cannot be inspected or the containers cannot be listed.
//   - No running Watchtower container joins its network namespace, as for a
//     Watchtower image without the Watchtower label.
//
// When the cgroup file names a container, as on cgroup v1 or with the host's
// cgroup namespace, that ID is returned instead of a joiner. Containers
// waiting to restart are not counted. Ephemeral orchestrators are not counted, since they join Watchtower's
// network namespace and carry the Watchtower image's label. A single joiner is
// returned even when a self-update renamed it as old, since only that
// container can be this one. Several joiners are told apart by their start
// time, then by their names.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for the inspect and the listing.
//   - fsys: Filesystem rooted at "/" holding /proc, read for the cgroup file
//     and the start time of this container's first process.
//   - client: Docker client that reads the containers.
//   - mountID: The container ID mountinfo names.
//
// Returns:
//   - container.ContainerID: Watchtower's own container ID.
//   - *container.Container: Watchtower's own container, or nil when it was
//     not read.
//   - error: Non-nil when several Watchtower containers join the network
//     namespace and none can be told apart.
func joiningWatchtower(
	log *zerolog.Logger,
	ctx context.Context,
	fsys fs.FS,
	client ContainerReader,
	mountID container.ContainerID,
) (container.ContainerID, *container.Container, error) {
	mounted, err := client.GetCurrentWatchtowerContainer(ctx, mountID)
	if err != nil {
		log.Debug().
			Err(err).
			Str("container_id", string(mountID)).
			Msg("Could not inspect the container named by mountinfo")

		return mountID, nil, nil
	}

	if mounted.IsWatchtower() {
		return mountID, mounted, nil
	}

	// The cgroup file belongs to this container in any network mode, so an
	// ID it names is exact.
	cgroupID, err := GetContainerIDFromCgroupFile(log, fsys)
	if err == nil {
		return cgroupID, nil, nil
	}

	watchtowers, err := client.ListLabeledContainers(ctx, labels.Watchtower)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Could not list the containers that join the network container named by mountinfo")

		return mountID, nil, nil
	}

	var joiners []*container.Container

	for _, c := range watchtowers {
		// Docker reports a container waiting to restart as running too.
		if c.IsRunning() && !c.IsRestarting() && !c.IsEphemeralOrchestrator() && c.JoinsNetworkOf(mounted) {
			joiners = append(joiners, c)
		}
	}

	var joiner *container.Container

	switch len(joiners) {
	case 0:
		return mountID, nil, nil
	case 1:
		joiner = joiners[0]
	default:
		// A rename-based self-update keeps the old container running until
		// its replacement removes it, so a replacement whose start cannot be
		// told apart takes the joiner that is not old. A restarted old
		// container that does the same removes itself in the replacement's
		// startup cleanup.
		joiner = cmp.Or(
			startedWithThisContainer(log, fsys, joiners),
			soleJoiner(joiners, func(c *container.Container) bool { return !container.IsSuperseded(c) }),
		)
		if joiner == nil {
			return "", nil, errSeveralWatchtowersJoinNetwork
		}
	}

	log.Debug().
		Str("network_container_id", string(mountID)).
		Msg("Mountinfo names the network container that Watchtower joins")

	return joiner.ID(), joiner, nil
}

// startedWithThisContainer returns the one joiner that started when this
// container's first process started.
//
// A restarted old Watchtower container and its replacement both join the
// network namespace, and Docker reports nothing else that tells them apart.
// Their recorded start times differ unless they started within about two
// seconds of each other.
//
// Parameters:
//   - log: Process logger.
//   - fsys: Filesystem rooted at "/" holding /proc.
//   - joiners: The running Watchtower containers that join the network
//     namespace.
//
// Returns:
//   - *container.Container: The matching joiner, or nil when the start time
//     cannot be read or no single joiner matches it.
func startedWithThisContainer(log *zerolog.Logger, fsys fs.FS, joiners []*container.Container) *container.Container {
	started, err := containerStartedAt(fsys)
	if err != nil {
		log.Debug().Err(err).Msg("Could not read when this container started")

		return nil
	}

	return soleJoiner(joiners, func(c *container.Container) bool {
		joinerStarted, ok := c.StartedAt()

		return ok && joinerStarted.Sub(started).Abs() <= startTimeTolerance
	})
}

// soleJoiner returns the one joiner that matches.
//
// Parameters:
//   - joiners: The running Watchtower containers that join the network
//     namespace.
//   - matches: Reports whether a joiner matches.
//
// Returns:
//   - *container.Container: The joiner, or nil when none or several match.
func soleJoiner(joiners []*container.Container, matches func(*container.Container) bool) *container.Container {
	var match *container.Container

	for _, c := range joiners {
		if !matches(c) {
			continue
		}

		if match != nil {
			return nil
		}

		match = c
	}

	return match
}

// containerStartedAt returns when the first process of this container's PID
// namespace started.
//
// Parameters:
//   - fsys: Filesystem rooted at "/" holding /proc.
//
// Returns:
//   - time.Time: The start time, accurate to about a second.
//   - error: Non-nil when /proc/stat or /proc/1/stat cannot be read or parsed.
func containerStartedAt(fsys fs.FS) (time.Time, error) {
	stat, err := fs.ReadFile(fsys, "proc/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("read /proc/stat: %w", err)
	}

	var bootTime int64

	for line := range strings.Lines(string(stat)) {
		value, found := strings.CutPrefix(line, "btime ")
		if found {
			bootTime, err = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("%w: %w", errNoBootTime, err)
			}

			break
		}
	}

	if bootTime == 0 {
		return time.Time{}, errNoBootTime
	}

	processStat, err := fs.ReadFile(fsys, "proc/1/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("read /proc/1/stat: %w", err)
	}

	// The command name is in parentheses and may hold spaces or
	// parentheses, so the fields are read after the last one.
	_, afterCommand, found := strings.CutLast(string(processStat), ")")
	if !found {
		return time.Time{}, errMalformedProcessStat
	}

	fields := strings.Fields(afterCommand)
	if len(fields) <= statStartTimeIndex {
		return time.Time{}, errMalformedProcessStat
	}

	ticks, err := strconv.ParseInt(fields[statStartTimeIndex], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errMalformedProcessStat, err)
	}

	// Each tick is converted before multiplying, since ticks times a second
	// in nanoseconds overflows once the host has been up for about 2.9 years.
	return time.Unix(bootTime, 0).Add(time.Duration(ticks) * (time.Second / userHZ)), nil
}

// InContainer reports whether the process runs in a container, based on the
// marker files that Docker and Podman create.
//
// A marker that cannot be checked for a reason other than not existing counts
// as present, so callers that protect other Watchtower containers fail closed.
//
// Parameters:
//   - fsys: The filesystem to check, rooted at "/".
//
// Returns:
//   - bool: True if a container marker exists or cannot be checked.
func InContainer(fsys fs.FS) bool {
	for _, marker := range containerMarkers {
		_, err := fs.Stat(fsys, marker)
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}

	return false
}

// GetContainerIDFromMountinfo retrieves the container ID from /proc/self/mountinfo.
// Uses the mountinfo file to find container paths containing /containers/<id>.
//
// Parameters:
//   - log: Process logger.
//   - fsys: Filesystem rooted at "/" holding /proc.
//
// Returns:
//   - container.ContainerID: The detected container ID.
//   - error: Non-nil when the file cannot be read or holds no container ID.
func GetContainerIDFromMountinfo(log *zerolog.Logger, fsys fs.FS) (container.ContainerID, error) {
	file, err := fs.ReadFile(fsys, "proc/self/mountinfo")
	if err != nil {
		log.Debug().
			Err(err).
			Str("file", "/proc/self/mountinfo").
			Msg("Failed to read mountinfo file")

		return "", errReadMountinfoFile
	}

	log.Debug().
		Str("file", "/proc/self/mountinfo").
		Msg("Read mountinfo file successfully")

	containerID, err := parseContainerIDFromMountinfo(log, string(file))
	if err != nil {
		log.Debug().
			Err(err).
			Str("file", "/proc/self/mountinfo").
			Msg("Failed to extract container ID from mountinfo")

		return "", errExtractContainerIDFromMountinfo
	}

	return containerID, nil
}

// parseContainerIDFromMountinfo parses the mountinfo string to extract container ID.
func parseContainerIDFromMountinfo(log *zerolog.Logger, mountinfoString string) (container.ContainerID, error) {
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
			log.Debug().
				Str("root", root).
				Msg("Processing mountinfo root")

			containerID := extractContainerIDFromPath(root)
			if containerID != "" {
				log.Debug().
					Str("id", string(containerID)).
					Msg("Extracted container ID from mountinfo root")

				return containerID, nil
			}
		}
	}

	return "", errNoValidContainerID
}

// extractContainerIDFromPath extracts container ID from a path containing /containers/<id>.
func extractContainerIDFromPath(path string) container.ContainerID {
	matches := containerIDPattern.FindStringSubmatch(path)
	if len(matches) >= minMatchGroups {
		return container.ContainerID(matches[1])
	}

	return ""
}

// GetContainerIDFromCgroupFile retrieves the container ID from /proc/<pid>/cgroup.
// Uses the cgroup file to find Docker and Podman container paths.
//
// Parameters:
//   - log: Process logger.
//   - fsys: Filesystem rooted at "/" holding /proc.
//
// Returns:
//   - container.ContainerID: The detected container ID.
//   - error: Non-nil when the file cannot be read or holds no container ID.
func GetContainerIDFromCgroupFile(log *zerolog.Logger, fsys fs.FS) (container.ContainerID, error) {
	filePath := fmt.Sprintf("/proc/%d/cgroup", os.Getpid())

	file, err := fs.ReadFile(fsys, strings.TrimPrefix(filePath, "/"))
	if err != nil {
		log.Debug().
			Err(err).
			Str("file", filePath).
			Msg("Failed to read cgroup file")

		return "", errReadCgroupFile
	}

	log.Debug().
		Str("file", filePath).
		Msg("Read cgroup file successfully")

	containerID, err := parseContainerIDFromCgroupString(log, string(file))
	if err != nil {
		log.Debug().
			Err(err).
			Str("file", filePath).
			Msg("Failed to extract container ID from cgroup")

		return "", errExtractContainerID
	}

	return containerID, nil
}

// parseContainerIDFromCgroupString parses the cgroup string to extract container ID.
//
// It reads the ID from a Docker path under the cgroupfs driver, /docker/<id>,
// or from a container's systemd scope under the systemd driver,
// docker-<id>.scope for Docker or libpod-<id>.scope for Podman.
func parseContainerIDFromCgroupString(log *zerolog.Logger, cgroupString string) (container.ContainerID, error) {
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
		if len(matches) < minMatchGroups {
			matches = scopeContainerPattern.FindStringSubmatch(trimmedLine)
		}

		log.Debug().
			Str("line", trimmedLine).
			Strs("matches", matches).
			Msg("Processed cgroup line for container ID")

		if len(matches) >= minMatchGroups {
			id := container.ContainerID(matches[1])
			log.Debug().
				Str("id", string(id)).
				Msg("Extracted container ID from cgroup")

			return id, nil
		}
	}

	return "", fmt.Errorf("%w: %q", errNoValidContainerID, cgroupString)
}

// GetContainerIDFromHostname retrieves the container ID by matching the HOSTNAME env var.
//
// It uses the Docker API to list containers and find a matching hostname.
// When multiple containers share the same hostname (e.g., in Docker Compose setups),
// it prefers the container with the Watchtower label.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: Docker client interface for container operations.
//
// Returns:
//   - container.ContainerID: The detected container ID if a match is found.
//   - error: Non-nil if the HOSTNAME env var is missing, the Docker client's
//     ListContainers call fails, or no matching container is found.
func GetContainerIDFromHostname(log *zerolog.Logger, ctx context.Context, client ContainerReader) (container.ContainerID, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		return "", errContainerIDNotFound
	}

	containers, err := client.ListContainers(ctx)
	if err != nil {
		return "", err //nolint:wrapcheck // The client's error already names the failed listing.
	}

	// Collect all containers matching the hostname.
	var (
		firstMatchID    container.ContainerID
		watchtowerMatch container.ContainerID
	)

	for _, c := range containers {
		containerInfo := c.ContainerInfo()
		if containerInfo == nil {
			log.Debug().Msg("Container info is nil, skipping hostname check")

			continue
		}

		if containerInfo.Config == nil {
			log.Debug().Msg("Container config is nil, skipping hostname check")

			continue
		}

		if containerInfo.Config.Hostname == hostname {
			// Record only the first match ID instead of accumulating full containers.
			if firstMatchID == "" {
				firstMatchID = c.ID()
			}

			// Prefer non-old WT to avoid selecting a lingering predecessor
			// as "current" for the running (successor) process.
			if c.IsWatchtower() {
				contName := c.Name()
				if contName == "" {
					contName = containerInfo.Name
				}

				isOld := container.IsOldContainer(contName)

				if watchtowerMatch == "" {
					watchtowerMatch = c.ID()
				} else if !isOld {
					watchtowerMatch = c.ID()
				}
			}
		}
	}

	// Return the Watchtower container if found among matches.
	if watchtowerMatch != "" {
		log.Debug().
			Str("container_id", string(watchtowerMatch)).
			Msg("Found Watchtower container by hostname with Watchtower label")

		return watchtowerMatch, nil
	}

	// Fall back to the first match if no Watchtower container was found.
	if firstMatchID != "" {
		log.Debug().
			Str("container_id", string(firstMatchID)).
			Msg("Found container by hostname (first match, no Watchtower label)")

		return firstMatchID, nil
	}

	return "", errNoContainerWithHostname
}
