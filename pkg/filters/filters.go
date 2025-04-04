// Package filters provides filtering logic for Watchtower containers.
// It defines various filter functions to select containers based on names, labels, scopes, and images.
package filters

import (
	"regexp"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// noScope is the default scope value when none is specified.
const noScope = "none"

// WatchtowerContainersFilter filters only watchtower containers.
func WatchtowerContainersFilter(c types.FilterableContainer) bool {
	clog := logrus.WithField("container", c.Name())
	isWatchtower := c.IsWatchtower()
	clog.WithField("is_watchtower", isWatchtower).Debug("Filtering for Watchtower container")

	return isWatchtower
}

// NoFilter will not filter out any containers.
func NoFilter(c types.FilterableContainer) bool {
	logrus.WithField("container", c.Name()).Debug("No filter applied")

	return true
}

// FilterByNames returns all containers that match one of the specified names.
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
			if name == c.Name() || name == c.Name()[1:] {
				clog.Debug("Matched container by exact name")

				return baseFilter(c)
			}

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

// FilterByDisableNames returns all containers that don't match any of the specified names.
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

// FilterByEnableLabel returns all containers that have the enabled label set.
func FilterByEnableLabel(baseFilter types.Filter) types.Filter {
	return func(c types.FilterableContainer) bool {
		// If label filtering is enabled, containers should only be considered
		// if the label is specifically set.
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

// FilterByDisabledLabel returns all containers that have the enabled label set to disable.
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

// FilterByScope returns all containers that belong to a specific scope.
func FilterByScope(scope string, baseFilter types.Filter) types.Filter {
	return func(c types.FilterableContainer) bool {
		clog := logrus.WithFields(logrus.Fields{
			"container": c.Name(),
			"scope":     scope,
		})

		containerScope, containerHasScope := c.Scope()
		if !containerHasScope || containerScope == "" {
			containerScope = noScope
		}

		if containerScope == scope {
			clog.WithField("container_scope", containerScope).Debug("Container matched scope")

			return baseFilter(c)
		}

		clog.WithField("container_scope", containerScope).Debug("Container scope mismatch")

		return false
	}
}

// FilterByImage returns all containers that have a specific image.
func FilterByImage(images []string, baseFilter types.Filter) types.Filter {
	if images == nil {
		return baseFilter
	}

	return func(c types.FilterableContainer) bool {
		clog := logrus.WithFields(logrus.Fields{
			"container": c.Name(),
			"images":    images,
		})

		image := strings.Split(c.ImageName(), ":")[0]
		if slices.Contains(images, image) {
			clog.WithField("image", image).Debug("Container matched image")

			return baseFilter(c)
		}

		clog.WithField("image", image).Debug("Container image did not match")

		return false
	}
}

// BuildFilter creates the needed filter of containers.
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

	stringBuilder := strings.Builder{}
	filter := NoFilter
	filter = FilterByNames(names, filter)
	filter = FilterByDisableNames(disableNames, filter)

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

	// If label filtering is enabled, containers should only be considered
	// if the label is specifically set.
	if enableLabel {
		filter = FilterByEnableLabel(filter)

		stringBuilder.WriteString("using enable label, ")
	}

	// If a scope has explicitly defined as "none", containers should only be considered
	// if they do not have a scope defined, or if it's explicitly set to "none".
	if scope == noScope {
		filter = FilterByScope(scope, filter)

		stringBuilder.WriteString(`without a scope, "`)
	} else if scope != "" {
		filter = FilterByScope(scope, filter)

		stringBuilder.WriteString(`in scope "`)
		stringBuilder.WriteString(scope)
		stringBuilder.WriteString(`", `)
	}

	filter = FilterByDisabledLabel(filter)

	filterDesc := "Checking all containers (except explicitly disabled with label)"
	if stringBuilder.Len() > 0 {
		filterDesc = "Only checking containers " + stringBuilder.String()
		filterDesc = filterDesc[:len(filterDesc)-2] // Remove last ", "
	}

	clog.WithField("filter_desc", filterDesc).Debug("Filter built")

	return filter, filterDesc
}
