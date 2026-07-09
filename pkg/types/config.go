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
	// EnableCheckAPI enables the check API (resolved from --http-api-endpoints).
	EnableCheckAPI bool
	// EnableConfigAPI enables the config API (resolved from --http-api-endpoints).
	EnableConfigAPI bool
	// EnableContainersAPI enables the containers API (resolved from --http-api-endpoints or deprecated --http-api-containers).
	EnableContainersAPI bool
	// EnableEventsAPI enables the events API (resolved from --http-api-endpoints).
	EnableEventsAPI bool
	// EnableHealthAPI enables health probes (resolved from --http-api-endpoints).
	EnableHealthAPI bool
	// EnableHistoryAPI enables the history API (resolved from --http-api-endpoints).
	EnableHistoryAPI bool
	// EnableImagesAPI enables the images API (resolved from --http-api-endpoints).
	EnableImagesAPI bool
	// EnableMetricsAPI enables the metrics API (resolved from --http-api-endpoints or deprecated --http-api-metrics).
	EnableMetricsAPI bool
	// EnableSwaggerAPI enables Swagger UI (resolved from --http-api-endpoints).
	EnableSwaggerAPI bool
	// EnableUpdateAPI enables the update API (resolved from --http-api-endpoints or deprecated --http-api-update).
	EnableUpdateAPI bool
	// UnblockHTTPAPI allows periodic polling alongside the HTTP API, set via the --http-api-periodic-polls flag.
	UnblockHTTPAPI bool
	// APIToken is the authentication token for HTTP API access, set via the --http-api-token flag.
	APIToken string
	// APIEventsToken is the authentication token for the events SSE endpoint, set via the --http-api-events-token flag.
	APIEventsToken string
	// APIHost is the host to bind the HTTP API to, set via the --http-api-host flag (defaults to empty string).
	APIHost string
	// APIPort is the port for the HTTP API server, set via the --http-api-port flag (defaults to "8080").
	APIPort string
	// APIRateLimit is the maximum authentication requests per minute per IP address, set via the --http-api-rate-limit flag (defaults to 60).
	APIRateLimit int
	// NoStartupMessage suppresses startup messages if true, set via the --no-startup-message flag.
	NoStartupMessage bool
	// TLSCertPath is the path to the TLS certificate file, set via the --http-api-tls-cert flag.
	TLSCertPath string
	// TLSKeyPath is the path to the TLS key file, set via the --http-api-tls-key flag.
	TLSKeyPath string
	// CORSAllowedOrigins is a list of allowed CORS origins for cross-origin requests, set via the --http-api-cors-origins flag.
	CORSAllowedOrigins []string
	// TrustedProxies is a list of trusted proxy IPs/CIDRs for reverse proxy support, set via the --http-api-trusted-proxies flag.
	TrustedProxies []string
	// ProxyHeader is the header to use for real client IP behind a reverse proxy, set via the --http-api-proxy-header flag.
	ProxyHeader string
}
