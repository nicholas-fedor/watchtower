package types

import (
	"github.com/spf13/cobra"
)

// RunConfig encapsulates the configuration parameters for the runMain function.
//
// It aggregates command-line flags and derived settings into a single structure, providing a cohesive way
// to pass configuration data through the CLI execution flow, ensuring all necessary parameters are accessible
// for update operations, API setup, and scheduling.
type RunConfig struct {
	// Command is the cobra.Command instance representing the executed command, providing access to parsed flags.
	Command *cobra.Command
	// Names is a slice of container names explicitly provided as positional arguments, used for filtering.
	Names []string
	// Filter is the types.Filter function determining which containers are processed during updates.
	Filter Filter
	// FilterDesc is a human-readable description of the applied filter, used in logging and notifications.
	FilterDesc string
	// RunOnce indicates whether to perform a single update and exit, set via the --run-once flag.
	RunOnce bool
	// UpdateOnStart enables an immediate update check on startup, then continues with periodic updates, set via the --update-on-start flag.
	UpdateOnStart bool
	// EnableUpdateAPI enables the HTTP update API endpoint, set via the --http-api-update flag.
	EnableUpdateAPI bool
	// EnableMetricsAPI enables the HTTP metrics API endpoint, set via the --http-api-metrics flag.
	EnableMetricsAPI bool
	// UnblockHTTPAPI allows periodic polling alongside the HTTP API, set via the --http-api-periodic-polls flag.
	UnblockHTTPAPI bool
	// APIToken is the authentication token for HTTP API access, set via the --http-api-token flag.
	APIToken string
	// APIHost is the host to bind the HTTP API to, set via the --http-api-host flag (defaults to empty string).
	APIHost string
	// APIPort is the port for the HTTP API server, set via the --http-api-port flag (defaults to "8080").
	APIPort string
	// NoStartupMessage suppresses startup messages if true, set via the --no-startup-message flag.
	NoStartupMessage bool
}
