package compose

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/rs/zerolog"
)

// Docker Compose labels.
const (
	// ComposeDependsOnLabel lists container names this container depends on from Docker Compose, comma-separated.
	ComposeDependsOnLabel = "com.docker.compose.depends_on"
	// ComposeProjectLabel specifies the project name of the container in Docker Compose.
	ComposeProjectLabel = "com.docker.compose.project"
	// ComposeServiceLabel specifies the service name of the container in Docker Compose.
	ComposeServiceLabel = "com.docker.compose.service"
	// ComposeContainerNumber specifies the container number of the container in Docker Compose.
	ComposeContainerNumber = "com.docker.compose.container-number"
)

// ParseDependsOnLabel parses the Docker Compose depends_on label value.
//
// It handles both JSON format (Docker Compose v2+) and comma-separated string format.
// Returns a slice of service names.
//
// Parameters:
//   - labelValue: The raw label value from com.docker.compose.depends_on.
//
// Returns:
//   - []string: List of service names.
func ParseDependsOnLabel(log *zerolog.Logger, labelValue string) []string {
	if labelValue == "" {
		return nil
	}

	clogVal := log.With().
		Str("label_value", labelValue).
		Logger()
	clog := &clogVal
	clog.Debug().Msg("Parsing compose depends-on label")

	// Try to parse as JSON first (Docker Compose v2+ format)
	if strings.HasPrefix(strings.TrimSpace(labelValue), "{") {
		var dependsOn map[string]any

		err := json.Unmarshal([]byte(labelValue), &dependsOn)
		if err != nil {
			clog.Debug().
				Err(err).
				Msg("Failed to parse as JSON, falling back to string parsing")
		} else {
			services := make([]string, 0, len(dependsOn))
			for service := range dependsOn {
				services = append(services, service)
			}
			// Sort for consistent ordering
			sort.Strings(services)
			clog.Debug().
				Strs("parsed_services", services).
				Msg("Parsed JSON format compose depends-on label")

			return services
		}
	}

	// Fall back to string parsing (legacy format)
	deps := strings.Split(labelValue, ",")
	services := make([]string, 0, len(deps))

	// Parse comma-separated list of service:condition:required
	for _, dep := range deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}

		clog.Debug().
			Str("parsing_dep", dep).
			Msg("Parsing individual dependency")
		// Parse colon-separated format: service:condition:required
		parts := strings.Split(dep, ":")

		serviceName := strings.TrimSpace(parts[0])
		if serviceName != "" {
			services = append(services, serviceName)
		}
	}

	clog.Debug().
		Strs("parsed_services", services).
		Msg("Completed parsing string format compose depends-on label")

	return services
}

// GetProjectName extracts the project name from Docker Compose labels.
//
// If the com.docker.compose.project label is present, returns its value.
// Otherwise, returns an empty string.
//
// Parameters:
//   - labels: Map of container labels.
//
// Returns:
//   - string: Project name if present, empty string otherwise.
func GetProjectName(log *zerolog.Logger, labels map[string]string) string {
	if labels == nil {
		return ""
	}

	projectName, ok := labels[ComposeProjectLabel]
	if !ok {
		return ""
	}

	log.Debug().
		Str("label", ComposeProjectLabel).
		Str("value", projectName).
		Msg("Retrieved compose project name")

	return projectName
}

// GetServiceName extracts the service name from Docker Compose labels.
//
// If the com.docker.compose.service label is present, returns its value.
// Otherwise, returns an empty string.
//
// Parameters:
//   - labels: Map of container labels.
//
// Returns:
//   - string: Service name if present, empty string otherwise.
func GetServiceName(log *zerolog.Logger, labels map[string]string) string {
	if labels == nil {
		return ""
	}

	serviceName, ok := labels[ComposeServiceLabel]
	if !ok {
		return ""
	}

	log.Debug().
		Str("label", ComposeServiceLabel).
		Str("value", serviceName).
		Msg("Retrieved compose service name")

	return serviceName
}

// GetContainerNumber extracts the container number from the Docker Compose labels.
//
// If the ComposeContainerNumber label is present, returns its value.
// Otherwise, returns an empty string.
//
// Parameters:
//   - labels: Map of container labels.
//
// Returns:
//   - string: Container replica number if present, empty string otherwise.
func GetContainerNumber(log *zerolog.Logger, labels map[string]string) string {
	if labels == nil {
		return ""
	}

	containerNumber, ok := labels[ComposeContainerNumber]
	if !ok {
		return ""
	}

	log.Debug().
		Str("label", ComposeContainerNumber).
		Str("value", containerNumber).
		Msg("Retrieved container replica number")

	return containerNumber
}
