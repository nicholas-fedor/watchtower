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
	// EnableCheckAPI enables the read-only check API endpoint, set via the --http-api-check flag.
	EnableCheckAPI bool
	// EnableMetricsAPI enables the HTTP metrics API endpoint, set via the --http-api-metrics flag.
	EnableMetricsAPI bool
	// EnableContainersAPI enables the read-only containers API endpoint, set via the --http-api-containers flag.
	EnableContainersAPI bool
	// EnableSwaggerAPI enables the Swagger UI endpoint, set via the --http-api-swagger flag.
	EnableSwaggerAPI bool
	// EnableHealthAPI enables the health probe endpoints, set via the --http-api-health flag.
	EnableHealthAPI bool
	// EnableHistoryAPI enables the scan history API endpoint, set via the --http-api-history flag.
	EnableHistoryAPI bool
	// EnableImagesAPI enables the images API endpoint, set via the --http-api-images flag.
	EnableImagesAPI bool
	// EnableConfigAPI enables the config API endpoint, set via the --http-api-config flag.
	EnableConfigAPI bool
	// EnableEventsAPI enables the real-time events API endpoint, set via the --http-api-events flag.
	EnableEventsAPI bool
	// EnableFullAPI enables all HTTP API endpoints, set via the --http-api-full flag.
	EnableFullAPI bool
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
