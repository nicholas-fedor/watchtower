package compose

import (
	"encoding/json"
	"maps"
	"slices"
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

	if strings.HasPrefix(strings.TrimSpace(labelValue), "{") {
		var dependsOn map[string]json.RawMessage

		err := json.Unmarshal([]byte(labelValue), &dependsOn)
		if err != nil {
			log.Debug().
				Err(err).
				Str("label_value", labelValue).
				Msg("Failed to parse as JSON, falling back to string parsing")
		} else {
			return slices.Sorted(maps.Keys(dependsOn))
		}
	}

	deps := strings.Split(labelValue, ",")
	services := make([]string, 0, len(deps))

	for _, dep := range deps {
		serviceName, _, _ := strings.Cut(strings.TrimSpace(dep), ":")

		serviceName = strings.TrimSpace(serviceName)
		if serviceName != "" {
			services = append(services, serviceName)
		}
	}

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
func GetProjectName(labels map[string]string) string {
	return labels[ComposeProjectLabel]
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
func GetServiceName(labels map[string]string) string {
	return labels[ComposeServiceLabel]
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
func GetContainerNumber(labels map[string]string) string {
	return labels[ComposeContainerNumber]
}
