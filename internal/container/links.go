package container

import (
	"slices"
	"strings"

	dockerContainer "github.com/moby/moby/api/types/container"

	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
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
// An entry that names the container by its own name is left out, and the
// second result reports that one was. Such an entry is a self-dependency, not
// a link to another container.
//
// Parameters:
//   - useComposeDependsOn: Whether to include Docker Compose depends_on label in dependency resolution.
//
// Returns:
//   - []string: List of linked container names with self-references removed.
//   - bool: True when a source named the container by its own name.
func (c *Container) Links(useComposeDependsOn bool) ([]string, bool) {
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

// NetworkContainer returns the container whose network namespace c joins.
//
// It is the name or ID the network mode names, such as vpn for
// --network container:vpn, without a leading slash. Inspecting a container
// replaces the ID Docker stores with the name, when that container still
// exists.
//
// Returns:
//   - string: The container's name or ID, empty when c has a network of its
//     own.
func (c *Container) NetworkContainer() string {
	info := c.ContainerInfo()
	if info == nil || info.HostConfig == nil || !info.HostConfig.NetworkMode.IsContainer() {
		return ""
	}

	return NormalizeContainerName(info.HostConfig.NetworkMode.ConnectedContainer())
}

// SetNetworkContainer sets the container whose network namespace the
// replacement of c joins.
//
// A scan that cannot inspect the network container leaves its ID in the
// network mode. Docker cannot resolve that ID once the network container is
// recreated, so its name is set instead. SetNetworkContainer does nothing when
// c has a network of its own.
//
// Parameters:
//   - name: The network container's name, without a leading slash.
func (c *Container) SetNetworkContainer(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.containerInfo == nil || c.containerInfo.HostConfig == nil ||
		!c.containerInfo.HostConfig.NetworkMode.IsContainer() {
		return
	}

	c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
		info.HostConfig.NetworkMode = dockerContainer.NetworkMode("container:" + name)
	})
}

// JoinsNetworkOf reports whether c joins the network namespace of other.
//
// Parameters:
//   - other: The container whose network namespace c may join.
//
// Returns:
//   - bool: True when c's network mode names other by name or ID.
func (c *Container) JoinsNetworkOf(other *Container) bool {
	networkContainer := c.NetworkContainer()

	return networkContainer != "" && other.isNamed(networkContainer)
}

// CreateConfigTargets returns the containers of others that c's host config
// names.
//
// Docker resolves the network container, legacy links and volumes-from of a
// new container when it creates it. A container created from c's config
// before one of these targets is recreated stays bound to the removed
// container, while creating it when a target is removed fails.
//
// Parameters:
//   - others: The containers c's host config may name.
//
// Returns:
//   - []*Container: The containers of others the host config names by name
//     or ID, in the order of others.
func (c *Container) CreateConfigTargets(others []*Container) []*Container {
	named := slices.Concat(legacyLinkNames(c), volumesFromLinks(c))
	if networkContainer := c.NetworkContainer(); networkContainer != "" {
		named = append(named, networkContainer)
	}

	var targets []*Container

	for _, other := range others {
		if other != c && slices.ContainsFunc(named, other.isNamed) {
			targets = append(targets, other)
		}
	}

	return targets
}

// isNamed reports whether a host config reference names c.
//
// Docker accepts a container's name or its full ID in a network mode, a legacy
// link or a volumes-from entry.
//
// Parameters:
//   - reference: The container named in the host config.
//
// Returns:
//   - bool: True when reference is c's name or ID.
func (c *Container) isNamed(reference string) bool {
	return reference == c.Name() || reference == string(c.ID())
}

// filterSelfReferences removes the links equal to containerName.
//
// Parameters:
//   - links: List of container names this container depends on.
//   - containerName: Name of the current container being checked.
//
// Returns:
//   - []string: Filtered list with self-references removed.
//   - bool: True when a link was removed.
func filterSelfReferences(links []string, containerName string) ([]string, bool) {
	filtered := make([]string, 0, len(links))

	for _, link := range links {
		if link != containerName {
			filtered = append(filtered, link)
		}
	}

	return filtered, len(filtered) < len(links)
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
func ResolveContainerIdentifier(c *Container) string {
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
func nameOrID(c *Container) string {
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

	services := composeLabels.ParseDependsOnLabel(c.Logger(), composeDependsOnLabelValue)

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
// It returns the legacy links and the network container. If the container
// has a project label, legacy link names are qualified with the project name
// if they are not already qualified. Network mode is a Docker identity rather
// than a Compose service name and is not project-prefixed. VolumesFrom is merged in
// Links, not here.
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

	legacyLinks := legacyLinkNames(c)
	networkContainer := c.NetworkContainer()

	// Pre-allocate for links plus a potential network mode dependency.
	normalizedLinks := make([]string, 0, len(legacyLinks)+1)

	for _, name := range legacyLinks {
		if projectName != "" && !strings.HasPrefix(name, projectName+"-") {
			name = projectName + "-" + name
		}

		normalizedLinks = append(normalizedLinks, name)
	}

	// Add network dependency.
	//
	// Unlike a Compose service reference, network mode is a Docker identity.
	// The daemon stores container:<id> after create. List inspect rewrites
	// that to container:<name>. Do not prefix either form with this
	// container's Compose project.
	if networkContainer != "" {
		normalizedLinks = append(normalizedLinks, networkContainer)
	}

	return normalizedLinks
}

// legacyLinkNames returns the names of the containers HostConfig.Links
// names, as Docker stores them.
//
// A malformed legacy link is skipped with a warning. Docker stores links as
// /name:/container/alias, so this only happens with inspect data Docker did
// not write.
//
// Parameters:
//   - c: Container instance.
//
// Returns:
//   - []string: The linked container names without a leading slash.
func legacyLinkNames(c *Container) []string {
	info := c.ContainerInfo()

	if info == nil || info.HostConfig == nil || len(info.HostConfig.Links) == 0 {
		return nil
	}

	names := make([]string, 0, len(info.HostConfig.Links))

	for _, link := range info.HostConfig.Links {
		if !strings.Contains(link, ":") {
			c.Logger().Warn().
				Str("container", c.Name()).
				Str("link", link).
				Msg("Invalid link format in host config, expected 'name:alias'")

			continue
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		if len(parts) < 1 || parts[0] == "" {
			c.Logger().Warn().
				Str("container", c.Name()).
				Str("link", link).
				Msg("Invalid link format in host config, missing container name")

			continue
		}

		names = append(names, NormalizeContainerName(parts[0]))
	}

	return names
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
		name, _ := ParseVolumesFromSpec(spec)
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

// ParseVolumesFromSpec splits a VolumesFrom entry into container identity and mode.
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
func ParseVolumesFromSpec(spec string) (string, string) {
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
