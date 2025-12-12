// Package compose provides functionality for handling Docker Compose-specific logic,
// including parsing depends_on labels and extracting service names for dependency management.
//
// Key components:
//   - ParseDependsOnLabel: Parses the Docker Compose depends_on label value,
//     expecting a comma-separated list of service:condition:required format,
//     returns a slice of service names.
//   - GetServiceName: Extracts the service name from Docker Compose labels
//     using the com.docker.compose.service label.
//
// Usage example:
//
//	services := compose.ParseDependsOnLabel("postgres:service_started:required,redis")
//	serviceName := compose.GetServiceName(labels)
package compose
