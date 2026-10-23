package container

import (
	"strings"

	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Links returns a list of container names this container depends on.
//
// Label dependencies come from one label, in priority order:
//  1. com.centurylinklabs.watchtower.depends-on label (Watchtower's native dependency format)
//  2. com.docker.compose.depends_on label (Docker Compose dependencies via v5 API)
//
// The host config dependencies are always appended after them, because the
// container cannot run without their targets:
//  1. HostConfig.Links (legacy Docker links)
//  2. NetworkMode.ConnectedContainer() (container network mode dependencies)
//  3. HostConfig.VolumesFrom identities
//
// Duplicates are skipped, so a target named by a label and by the host config
// appears once, in the label's position.
//
// Self-references are filtered out from all link sources to prevent circular
// dependencies where a container would depend on itself. This ensures the
// dependency resolution algorithm can process containers in a valid topological order.
//
// Parameters:
//   - useComposeDependsOn: Whether to include Docker Compose depends_on label in dependency resolution.
//
// Returns:
//   - []string: List of linked container names with self-references removed.
func (c *Container) Links(useComposeDependsOn bool) []string {
	var links []string

	// Check Watchtower's depends-on label first.
	if labelLinks := GetLinksFromWatchtowerLabel(c); labelLinks != nil {
		links = labelLinks
	} else if useComposeDependsOn {
		// Use Compose depends_on only when the Watchtower label is absent.
		if composeLinks := getLinksFromComposeLabel(c); composeLinks != nil {
			links = composeLinks
		}
	}

	// Always append the host config dependencies. A label must not hide the
	// container whose network or volumes this container uses, or the target
	// can be recreated while this container still uses it.
	links = appendUniqueLinks(links, getLinksFromHostConfig(c))
	links = appendUniqueLinks(links, volumesFromLinks(c))

	return filterSelfReferences(links, c.Name())
}

// filterSelfReferences removes any links that reference the container itself.
//
// This prevents circular dependencies where a container would depend on itself,
// which could cause infinite loops during dependency resolution and update processing.
// Self-references are filtered out to ensure the dependency graph remains acyclic
// and containers are processed in the correct order.
//
// Parameters:
//   - links: List of container names this container depends on.
//   - containerName: Name of the current container being checked.
//
// Returns:
//   - []string: Filtered list with self-references removed.
func filterSelfReferences(links []string, containerName string) []string {
	filtered := make([]string, 0, len(links))
	for _, link := range links {
		// Skip links that reference the container itself to prevent self-dependencies
		if link != containerName {
			filtered = append(filtered, link)
		}
	}

	return filtered
}

// ResolveContainerIdentifier returns a standardized container identifier used
// for dependency resolution, update coordination, logging, and cycle detection.
//
// Container identifier formats:
//  1. project-service-containerNumber (if project name, service name, and container number are all available)
//  2. project-service (if project name and service name are available)
//  3. service (if only service name is available)
//  4. container name (if name is available)
//  5. container ID (fallback)
//
// Parameters:
//   - c: Container to get identifier for
//
// Returns:
//   - string: Container identifier formatted according to the prioritization
//     order, always returns a non-empty string for valid containers
func ResolveContainerIdentifier(c types.Container) string {
	info := c.ContainerInfo()
	if info == nil {
		return nameOrID(c)
	}

	cfg := info.Config
	if cfg == nil {
		return nameOrID(c)
	}

	labels := cfg.Labels
	if len(labels) == 0 {
		return nameOrID(c)
	}

	projectName := composeLabels.GetProjectName(labels)
	serviceName := composeLabels.GetServiceName(labels)
	containerNumber := composeLabels.GetContainerNumber(labels)

	// Handle replica containers
	if projectName != "" && serviceName != "" &&
		strings.HasPrefix(c.Name(), projectName+"-"+serviceName+"-") {
		return c.Name()
	}

	// Prioritize identifier formats based on available information
	if projectName != "" && serviceName != "" && containerNumber != "" {
		return projectName + "-" + serviceName + "-" + containerNumber
	}

	if projectName != "" && serviceName != "" {
		return projectName + "-" + serviceName
	}

	if serviceName != "" {
		return serviceName
	}

	return nameOrID(c)
}

// nameOrID returns the container name if non-empty; otherwise, returns the container ID.
func nameOrID(c types.Container) string {
	// Return the container name if available.
	name := c.Name()
	if name != "" {
		return name
	}

	// Otherwise, return the container ID.
	return string(c.ID())
}

// GetLinksFromWatchtowerLabel extracts dependency links from the
// watchtower depends-on label.
//
// It parses the com.centurylinklabs.watchtower.depends-on label value,
// splitting on commas and normalizing each container name, returning all
// normalized links, including potential self-references.
//
// Note: Watchtower depends-on labels reference container names directly,
// unlike Compose depends-on, which references services within the same project.
// Therefore, we do not prefix with project name for Watchtower labels.
//
// Parameters:
//   - c: Container instance
//
// Returns:
//   - []string: List of all normalized links, including potential self-references, or nil if label not present
func GetLinksFromWatchtowerLabel(c *Container) []string {
	// Get the depends-on label value or empty string if not present
	dependsOnLabelValue := c.getLabelValueOrEmpty(labels.DependsOn)

	// If the label is empty, return nil
	if dependsOnLabelValue == "" {
		return nil
	}

	// Split the comma-separated values
	links := strings.Split(dependsOnLabelValue, ",")

	// Parse the links and normalize them
	normalizedLinks := make([]string, 0, len(links))
	for _, normalizedLink := range links {
		// Skip empty links
		if normalizedLink == "" {
			continue
		}

		// Normalize the link by trimming spaces and removing any leading slashes
		normalizedLink = NormalizeContainerName(strings.TrimSpace(normalizedLink))

		// Add the normalized link to the result slice
		normalizedLinks = append(normalizedLinks, normalizedLink)
	}

	return normalizedLinks
}

// getLinksFromComposeLabel extracts dependency links from the Docker Compose depends-on label.
//
// It parses the com.docker.compose.depends_on label value using the compose package,
// and normalizes each service name. If the container has a project label, service names
// are qualified with the project name.
//
// Parameters:
//   - c: Container instance
//
// Returns:
//   - []string: List of linked container names, empty if label not present
func getLinksFromComposeLabel(c *Container) []string {
	info := c.ContainerInfo()

	composeDependsOnLabelValue := c.getLabelValueOrEmpty(composeLabels.ComposeDependsOnLabel)
	if composeDependsOnLabelValue == "" {
		return nil
	}

	services := composeLabels.ParseDependsOnLabel(c.logger(), composeDependsOnLabelValue)

	projectName := composeLabels.GetProjectName(info.Config.Labels)

	normalizedLinks := make([]string, 0, len(services))
	for _, service := range services {
		normalizedService := NormalizeContainerName(service)
		// If the project name isn't empty and the service name doesn't have the project name prefix,
		// then add the project name prefix to the service name.
		if projectName != "" && !strings.HasPrefix(normalizedService, projectName+"-") {
			normalizedService = projectName + "-" + normalizedService
		}

		normalizedLinks = append(normalizedLinks, normalizedService)
	}

	if len(normalizedLinks) == 0 {
		return nil
	}

	return normalizedLinks
}

// getLinksFromHostConfig extracts dependency links from Docker HostConfig.
//
// It parses HostConfig.Links and network mode to determine container
// dependencies. If the container has a project label, legacy link names are
// qualified with the project name if they are not already qualified. Network
// mode is a Docker identity, not a Compose service name, and is not
// project-prefixed. VolumesFrom is merged in Links, not here.
//
// A malformed legacy link is skipped with a warning. Docker stores links as
// /name:/container/alias, so this only happens with inspect data Docker did
// not write.
//
// Parameters:
//   - c: Container instance
//
// Returns:
//   - []string: List of linked container names
func getLinksFromHostConfig(c *Container) []string {
	info := c.ContainerInfo()

	if info == nil || info.HostConfig == nil {
		return nil
	}

	var labels map[string]string
	if info.Config != nil {
		labels = info.Config.Labels
	}

	projectName := composeLabels.GetProjectName(labels)

	hostConfig := info.HostConfig

	// Pre-allocate for links plus a potential network mode dependency.
	capacity := len(hostConfig.Links)

	networkMode := hostConfig.NetworkMode
	if networkMode.IsContainer() {
		capacity++
	}

	normalizedLinks := make([]string, 0, capacity)

	for _, link := range hostConfig.Links {
		if !strings.Contains(link, ":") {
			c.logger().Warn().
				Str("container", c.Name()).
				Str("link", link).
				Msg("Invalid link format in host config, expected 'name:alias'")

			continue
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		if len(parts) < 1 || parts[0] == "" {
			c.logger().Warn().
				Str("container", c.Name()).
				Str("link", link).
				Msg("Invalid link format in host config, missing container name")

			continue
		}

		normalizedName := NormalizeContainerName(parts[0])
		if projectName != "" && !strings.HasPrefix(normalizedName, projectName+"-") {
			normalizedName = projectName + "-" + normalizedName
		}

		normalizedLinks = append(normalizedLinks, normalizedName)
	}

	// Add network dependency.
	//
	// Unlike a Compose service reference, network mode is a Docker identity.
	// The daemon stores container:<id> after create. List inspect rewrites
	// that to container:<name>. Do not prefix either form with this
	// container's Compose project.
	if networkMode.IsContainer() {
		normalizedName := NormalizeContainerName(networkMode.ConnectedContainer())
		normalizedLinks = append(normalizedLinks, normalizedName)
	}

	return normalizedLinks
}

// volumesFromLinks returns normalized HostConfig.VolumesFrom identities.
//
// VolumesFrom is a Docker identity (name or ID, optional :ro/:rw). Do not
// prefix with this container's Compose project.
//
// Parameters:
//   - c: Container instance.
//
// Returns:
//   - []string: Normalized volumes-from container names or IDs.
func volumesFromLinks(c *Container) []string {
	info := c.ContainerInfo()

	if info == nil || info.HostConfig == nil {
		return nil
	}

	specs := info.HostConfig.VolumesFrom
	if len(specs) == 0 {
		return nil
	}

	links := make([]string, 0, len(specs))
	for _, spec := range specs {
		name, _ := parseVolumesFromSpec(spec)
		if name == "" {
			// Skip empty or mode-only specs.
			continue
		}

		links = append(links, NormalizeContainerName(name))
	}

	return links
}

// appendUniqueLinks appends extra names that are not already in links.
//
// Existing order is preserved. New names are appended in extra order.
//
// Parameters:
//   - links: Existing dependency names.
//   - extra: Additional names to merge.
//
// Returns:
//   - []string: Merged list without duplicates.
func appendUniqueLinks(links, extra []string) []string {
	if len(extra) == 0 {
		return links
	}

	seen := make(map[string]struct{}, len(links)+len(extra))
	for _, link := range links {
		seen[link] = struct{}{}
	}

	for _, link := range extra {
		if _, exists := seen[link]; exists {
			// Keep the earlier occurrence so label order wins.
			continue
		}

		seen[link] = struct{}{}
		links = append(links, link)
	}

	return links
}

// parseVolumesFromSpec splits a VolumesFrom entry into container identity and mode.
//
// Docker stores entries as name-or-id, optionally followed by :ro, :rw, or a
// SELinux :z/:Z flag. A trailing colon suffix is treated as a mode only when
// every comma-separated part is one of those flags.
//
// Parameters:
//   - spec: VolumesFrom string from HostConfig.
//
// Returns:
//   - string: Container name or ID.
//   - string: Mode suffix without a leading colon, or empty.
func parseVolumesFromSpec(spec string) (string, string) {
	if spec == "" {
		return "", ""
	}

	before, after, ok := strings.CutLast(spec, ":")
	if !ok {
		return spec, ""
	}

	suffix := after
	if !isVolumesFromMode(suffix) {
		// A colon that is not a volume mode stays part of the identity.
		return spec, ""
	}

	return before, suffix
}

// isVolumesFromMode reports whether suffix is a Docker volumes-from access mode.
//
// Docker accepts ro, rw, and SELinux z or Z, including comma-separated
// combinations such as ro,z.
//
// Parameters:
//   - suffix: Trailing VolumesFrom mode after the last colon.
//
// Returns:
//   - bool: True if every comma-separated part is ro, rw, z, or Z.
func isVolumesFromMode(suffix string) bool {
	if suffix == "" {
		return false
	}

	for part := range strings.SplitSeq(suffix, ",") {
		switch part {
		case "ro", "rw", "z", "Z":
			// Recognized volume access or SELinux relabel flag.
		default:
			return false
		}
	}

	return true
}
