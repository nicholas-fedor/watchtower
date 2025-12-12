// Package compose provides functionality for handling Docker Compose-specific logic,
// including parsing depends_on labels and extracting service names for dependency management.
package compose

import (
	"strings"

	"github.com/sirupsen/logrus"
)

// Docker Compose labels.
const (
	// ComposeDependsOnLabel lists container names this container depends on from Docker Compose, comma-separated.
	ComposeDependsOnLabel = "com.docker.compose.depends_on"
	// ComposeServiceLabel specifies the service name of the container in Docker Compose.
	ComposeServiceLabel = "com.docker.compose.service"
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
		if len(parts) >= 1 {
			serviceName := strings.TrimSpace(parts[0])
			if serviceName != "" {
				clog.WithField("parsed_service", serviceName).
					Debug("Added parsed service to dependencies")
				services = append(services, serviceName)
			}
		}
	}

	clog.WithField("parsed_services", services).Debug("Completed parsing compose depends-on label")

	return services
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
		logrus.WithField("label", ComposeServiceLabel).Debug("Compose service label not found")

		return ""
	}

	logrus.WithFields(logrus.Fields{
		"label": ComposeServiceLabel,
		"value": serviceName,
	}).Debug("Retrieved compose service name")

	return serviceName
}
