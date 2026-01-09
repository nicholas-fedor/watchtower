package compose

import (
	"strings"

	"github.com/sirupsen/logrus"
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
// It expects a comma-separated list of service:condition:required format.
// Returns a slice of service names.
//
// Parameters:
//   - labelValue: The raw label value from com.docker.compose.depends_on.
//
// Returns:
//   - []string: List of service names.
func ParseDependsOnLabel(labelValue string) []string {
	if labelValue == "" {
		return nil
	}

	deps := strings.Split(labelValue, ",")
	services := make([]string, 0, len(deps))

	clog := logrus.WithField("label_value", labelValue)

	clog.Debug("Parsing compose depends-on label")
	// Parse comma-separated list of service:condition:required
	for _, dep := range deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}

		clog.WithField("parsing_dep", dep).Debug("Parsing individual dependency")
		// Parse colon-separated format: service:condition:required
		parts := strings.Split(dep, ":")

		serviceName := strings.TrimSpace(parts[0])
		if serviceName != "" {
			services = append(services, serviceName)
		}
	}

	clog.WithField("parsed_services", services).Debug("Completed parsing compose depends-on label")

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
	if labels == nil {
		return ""
	}

	projectName, ok := labels[ComposeProjectLabel]
	if !ok {
		return ""
	}

	logrus.WithFields(logrus.Fields{
		"label": ComposeProjectLabel,
		"value": projectName,
	}).Debug("Retrieved compose project name")

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
func GetServiceName(labels map[string]string) string {
	if labels == nil {
		return ""
	}

	serviceName, ok := labels[ComposeServiceLabel]
	if !ok {
		return ""
	}

	logrus.WithFields(logrus.Fields{
		"label": ComposeServiceLabel,
		"value": serviceName,
	}).Debug("Retrieved compose service name")

	return serviceName
}

// GetContainerNumber extracts the container number from the Docker Compose labels.
//
// If the com.docker.compose.container-number is present, returns its value.
// Otherwise, returns an empty string.
//
// Parameters:
//   - string: Container replica number if present, empty string otherwise.
func GetContainerNumber(labels map[string]string) string {
	if labels == nil {
		return ""
	}

	containerNumber, ok := labels[ComposeContainerNumber]
	if !ok {
		return ""
	}

	logrus.WithFields(logrus.Fields{
		"label": ComposeContainerNumber,
		"value": containerNumber,
	}).Debug("Retrieved container replica number")

	return containerNumber
}
