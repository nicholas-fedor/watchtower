// Package filters provides filtering logic for Watchtower containers.
// It defines various filter functions to select containers based on names, labels, scopes, and images.
package filters

import (
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// noScope is the default scope value when none is specified.
const noScope = "none"

// WatchtowerContainersFilter selects only Watchtower containers.
//
// Returns:
//   - bool: True if container is Watchtower, false otherwise.
func WatchtowerContainersFilter(c types.FilterableContainer) bool {
	clog := logrus.WithField("container", c.Name())
	isWatchtower := c.IsWatchtower()
	clog.WithField("is_watchtower", isWatchtower).Debug("Filtering for Watchtower container")

	return isWatchtower
}

// UnscopedWatchtowerContainersFilter selects only unscoped Watchtower containers.
//
// Returns:
//   - bool: True if container is Watchtower and has no scope or scope "none", false otherwise.
func UnscopedWatchtowerContainersFilter(c types.FilterableContainer) bool {
	clog := logrus.WithField("container", c.Name())

	if !c.IsWatchtower() {
		clog.Debug("Container is not Watchtower")

		return false
	}

	containerScope, containerHasScope := c.Scope()
	if !containerHasScope || containerScope == "" {
		containerScope = noScope // Default to "none" if unset.
	}

	if containerScope == noScope {
		clog.WithField("container_scope", containerScope).
			Debug("Filtering for unscoped Watchtower container")

		return true
	}

	clog.WithField("container_scope", containerScope).
		Debug("Container has scope, excluding from unscoped filter")

	return false
}

// NoFilter allows all containers through.
//
// Returns:
//   - bool: Always true.
func NoFilter(c types.FilterableContainer) bool {
	logrus.WithField("container", c.Name()).Debug("No filter applied")

	return true
}

// FilterByNames selects containers matching specified names.
//
// Parameters:
//   - names: List of names or regex patterns to match.
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function combining name check with base filter.
func FilterByNames(names []string, baseFilter types.Filter) types.Filter {
	if len(names) == 0 {
		return baseFilter
	}

	return func(c types.FilterableContainer) bool {
		clog := logrus.WithFields(logrus.Fields{
			"container": c.Name(),
			"names":     names,
		})

		for _, name := range names {
			// Match exact name with or without leading slash.
			if name == c.Name() || name == c.Name()[1:] {
				clog.Debug("Matched container by exact name")

				return baseFilter(c)
			}

			// Try regex match if name is a pattern.
			if re, err := regexp.Compile(name); err == nil {
				indices := re.FindStringIndex(c.Name())
				if indices == nil {
					continue
				}

				start := indices[0]
				end := indices[1]

				if start <= 1 && end >= len(c.Name())-1 {
					clog.Debug("Matched container by regex")

					return baseFilter(c)
				}
			} else {
				clog.WithError(err).Warn("Invalid regex in name filter")
			}
		}

		clog.Debug("Container name did not match any filter")

		return false
	}
}

// FilterByDisableNames excludes containers matching specified names.
//
// Parameters:
//   - disableNames: Names to exclude.
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function excluding names and applying base filter.
func FilterByDisableNames(disableNames []string, baseFilter types.Filter) types.Filter {
	if len(disableNames) == 0 {
		return baseFilter
	}

	return func(c types.FilterableContainer) bool {
		clog := logrus.WithFields(logrus.Fields{
			"container":    c.Name(),
			"disableNames": disableNames,
		})

		for _, name := range disableNames {
			if name == c.Name() || name == c.Name()[1:] {
				clog.Debug("Container excluded by disable name")

				return false
			}
		}

		clog.Debug("Container not excluded by disable names")

		return baseFilter(c)
	}
}

// FilterByEnableLabel selects containers with enable label set.
//
// Parameters:
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function requiring enable label and applying base filter.
func FilterByEnableLabel(baseFilter types.Filter) types.Filter {
	return func(c types.FilterableContainer) bool {
		clog := logrus.WithField("container", c.Name())
		_, ok := c.Enabled()

		if !ok {
			clog.Debug("Container excluded: enable label not set")

			return false
		}

		clog.Debug("Container included: enable label set")

		return baseFilter(c)
	}
}

// FilterByDisabledLabel excludes containers with enable label set to false.
//
// Parameters:
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function excluding disabled containers and applying base filter.
func FilterByDisabledLabel(baseFilter types.Filter) types.Filter {
	return func(c types.FilterableContainer) bool {
		clog := logrus.WithField("container", c.Name())
		enabledLabel, ok := c.Enabled()

		if ok && !enabledLabel {
			clog.Debug("Container excluded: enable label set to false")

			return false
		}

		clog.Debug("Container not excluded by disabled label")

		return baseFilter(c)
	}
}

// FilterByScope selects containers in a specific scope.
//
// Parameters:
//   - scope: Scope to match.
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function matching scope and applying base filter.
func FilterByScope(scope string, baseFilter types.Filter) types.Filter {
	return func(c types.FilterableContainer) bool {
		clog := logrus.WithFields(logrus.Fields{
			"container": c.Name(),
			"scope":     scope,
		})

		containerScope, containerHasScope := c.Scope()
		if !containerHasScope || containerScope == "" {
			containerScope = noScope // Default to "none" if unset.
		}

		if containerScope == scope {
			clog.WithField("container_scope", containerScope).Debug("Container matched scope")

			return baseFilter(c)
		}

		clog.WithField("container_scope", containerScope).Debug("Container scope mismatch")

		return false
	}
}

// FilterByImage selects containers with specific images, optionally including tags.
//
// Parameters:
//   - images: List of image names (with optional tags) to match.
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function matching images and applying base filter.
func FilterByImage(images []string, baseFilter types.Filter) types.Filter {
	if images == nil {
		return baseFilter // No images specified, apply base filter only.
	}

	return func(c types.FilterableContainer) bool {
		clog := logrus.WithFields(logrus.Fields{
			"container": c.Name(),
			"images":    images,
		})

		for _, targetImage := range images {
			if matchImageAndTag(c.ImageName(), targetImage) {
				clog.WithField("image", c.ImageName()).Debug("Container matched image")

				return baseFilter(c) // Image matches, proceed with base filter.
			}
		}

		clog.WithField("image", c.ImageName()).Debug("Container image did not match")

		return false // No matching image found.
	}
}

// matchImageAndTag checks if a container's image matches a target image, including optional tag.
//
// Parameters:
//   - containerImage: The container's image name (e.g., "registry:develop").
//   - targetImage: The target image name or image:tag to match (e.g., "registry").
//
// Returns:
//   - bool: True if the image (and tag, if specified) matches, false otherwise.
func matchImageAndTag(containerImage, targetImage string) bool {
	containerParts := strings.Split(containerImage, ":") // Split into name and tag.
	targetParts := strings.Split(targetImage, ":")       // Split target into name and tag.

	if containerParts[0] != targetParts[0] { // Compare image names.
		return false // Image names don't match.
	}

	if len(targetParts) > 1 && len(containerParts) > 1 { // Both have tags.
		return containerParts[1] == targetParts[1] // Compare tags.
	}

	return true // No tag in target or container, name match sufficient.
}

// BuildFilter constructs a composite filter for containers.
//
// Parameters:
//   - names: Names to include.
//   - disableNames: Names to exclude.
//   - enableLabel: Require enable label if true.
//   - scope: Scope to match.
//
// Returns:
//   - types.Filter: Combined filter function.
//   - string: Description of the filter.
func BuildFilter(
	names []string,
	disableNames []string,
	enableLabel bool,
	scope string,
) (types.Filter, string) {
	clog := logrus.WithFields(logrus.Fields{
		"names":        names,
		"disableNames": disableNames,
		"enableLabel":  enableLabel,
		"scope":        scope,
	})
	clog.Debug("Building container filter")

	// Start with no filter and chain additional filters.
	stringBuilder := strings.Builder{}
	filter := NoFilter
	filter = FilterByNames(names, filter)
	filter = FilterByDisableNames(disableNames, filter)

	// Add name-based filter description.
	if len(names) > 0 {
		stringBuilder.WriteString("which name matches \"")

		for i, n := range names {
			stringBuilder.WriteString(n)

			if i < len(names)-1 {
				stringBuilder.WriteString(`" or "`)
			}
		}

		stringBuilder.WriteString(`", `)
	}

	// Add disable-name-based filter description.
	if len(disableNames) > 0 {
		stringBuilder.WriteString("not named one of \"")

		for i, n := range disableNames {
			stringBuilder.WriteString(n)

			if i < len(disableNames)-1 {
				stringBuilder.WriteString(`" or "`)
			}
		}

		stringBuilder.WriteString(`", `)
	}

	// Apply enable label filter if specified.
	if enableLabel {
		filter = FilterByEnableLabel(filter)

		stringBuilder.WriteString("using enable label, ")
	}

	// Apply scope filter based on value.
	if scope == noScope { // "none"
		filter = FilterByScope(scope, filter)

		stringBuilder.WriteString(`without a scope, "`)
	} else if scope != "" {
		filter = FilterByScope(scope, filter)

		stringBuilder.WriteString(`in scope "`)
		stringBuilder.WriteString(scope)
		stringBuilder.WriteString(`", `)
	}

	// Exclude explicitly disabled containers.
	filter = FilterByDisabledLabel(filter)

	// Build filter description.
	filterDesc := "Checking all containers (except explicitly disabled with label)"
	if stringBuilder.Len() > 0 {
		filterDesc = "Only checking containers " + stringBuilder.String()
		filterDesc = filterDesc[:len(filterDesc)-2] // Trim trailing ", ".
	}

	clog.WithField("filter_desc", filterDesc).Debug("Filter built")

	return filter, filterDesc
}
