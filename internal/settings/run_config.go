package settings

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/api"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// errInvalidAPIHost indicates http-api-host is neither empty nor a valid IP.
var errInvalidAPIHost = errors.New(
	"invalid http-api-host: must be empty or a valid IP address (IPv4 or IPv6)",
)

// RunConfig encapsulates the configuration parameters for the runMain function.
type RunConfig struct {
	// Command is the cobra.Command instance representing the executed command, providing access to parsed flags.
	Command *cobra.Command
	// Filter is the types.Filter function determining which containers are processed during updates.
	Filter types.Filter
	// FilterDesc is a human-readable description of the applied filter, used in logging and notifications.
	FilterDesc string
	// APIToken is the authentication token for HTTP API access.
	APIToken string
	// APIEventsToken is the authentication token for the events SSE endpoint.
	APIEventsToken string
	// APIHost is the host interface to bind the HTTP API to (default: empty string).
	APIHost string
	// APIPort is the port for the HTTP API server (defaults to "8080").
	APIPort string
	// TLSCertPath is the path to the TLS certificate file.
	TLSCertPath string
	// TLSKeyPath is the path to the TLS key file.
	TLSKeyPath string
	// ProxyHeader is the header to use for real client IP behind a reverse proxy.
	ProxyHeader string
	// Names is a slice of container names explicitly provided as positional arguments, used for filtering.
	Names []string
	// CORSAllowedOrigins is a list of allowed CORS origins for cross-origin requests.
	CORSAllowedOrigins []string
	// TrustedProxies is a list of trusted proxy IPs/CIDRs for reverse proxy support.
	TrustedProxies []string
	// APIRateLimit is the maximum authentication requests per minute per IP address (default: 60).
	APIRateLimit int
	// CheckAPITimeout is the maximum duration for the /v1/check API endpoint.
	CheckAPITimeout time.Duration
	// UpdateAPITimeout is the maximum duration for the /v1/update API endpoint.
	UpdateAPITimeout time.Duration
	// RunOnce indicates whether to perform a single update and exit.
	RunOnce bool
	// UpdateOnStart enables an immediate update check on startup, then continues with periodic updates.
	UpdateOnStart bool
	// EnableCheckAPI enables the check API endpoint.
	EnableCheckAPI bool
	// EnableConfigAPI enables the config API endpoint.
	EnableConfigAPI bool
	// EnableContainersAPI enables the containers API endpoint.
	EnableContainersAPI bool
	// EnableEventsAPI enables the events API endpoint.
	EnableEventsAPI bool
	// EnableHealthAPI enables the HTTP API health probes.
	EnableHealthAPI bool
	// EnableHistoryAPI enables the history API endpoint.
	EnableHistoryAPI bool
	// EnableImagesAPI enables the images API endpoint.
	EnableImagesAPI bool
	// EnableMetricsAPI enables the metrics API endpoint.
	EnableMetricsAPI bool
	// EnableSwaggerAPI enables Swagger UI endpoint.
	EnableSwaggerAPI bool
	// EnableUpdateAPI enables the update API endpoint.
	EnableUpdateAPI bool
	// UnblockHTTPAPI allows periodic polling alongside the HTTP API.
	UnblockHTTPAPI bool
	// NoStartupMessage suppresses startup messages if true.
	NoStartupMessage bool
	// APIHostChanged reports whether http-api-host was explicitly configured.
	APIHostChanged bool
	// APIPortChanged reports whether http-api-port was explicitly configured.
	APIPortChanged bool
	// APIRateLimitChanged reports whether http-api-rate-limit was explicitly configured.
	APIRateLimitChanged bool
	// CheckAPITimeoutChanged reports whether http-api-check-timeout was explicitly configured.
	CheckAPITimeoutChanged bool
	// UpdateAPITimeoutChanged reports whether http-api-update-timeout was explicitly configured.
	UpdateAPITimeoutChanged bool
}

// RunConfigInput supplies runtime values needed to project Config into RunConfig.
type RunConfigInput struct {
	// Command is the cobra command retained on RunConfig for callers that still
	// carry a command reference through orchestration (not used for policy flag reads).
	Command *cobra.Command
	// Names are normalized container names for filtering display.
	Names []string
}

// BuildRunConfig projects Config into RunConfig for process orchestration.
//
// It applies the API port default and validates the API host. It resolves
// endpoint enablement from cfg.API and returns a complete RunConfig snapshot.
//
// Parameters:
//   - input: Runtime inputs (command pointer and normalized names).
//
// Returns:
//   - RunConfig: Orchestration snapshot for runMain.
//   - error: Non-nil when API host or endpoint configuration is invalid.
func (c Config) BuildRunConfig(input RunConfigInput) (RunConfig, error) {
	// Default port when unset so HTTP API bind always has a concrete value.
	apiPort := c.API.Port
	if apiPort == "" {
		apiPort = "8080"
	}

	err := ValidateAPIHost(c.API.Host)
	if err != nil {
		return RunConfig{}, err
	}

	endpointSet, err := api.ResolveEndpoints(
		c.API.Endpoints,
		c.API.LegacyUpdate,
		c.API.LegacyMetrics,
		c.API.LegacyContainers,
	)
	if err != nil {
		return RunConfig{}, fmt.Errorf("invalid HTTP API endpoint configuration: %w", err)
	}

	cfg := RunConfig{
		Command:                 input.Command,
		Names:                   append([]string(nil), input.Names...),
		Filter:                  c.Filter.Predicate,
		FilterDesc:              c.Filter.Desc,
		RunOnce:                 c.Mode.RunOnce,
		UpdateOnStart:           c.Schedule.UpdateOnStart,
		TLSCertPath:             c.API.TLSCert,
		TLSKeyPath:              c.API.TLSKey,
		CORSAllowedOrigins:      append([]string(nil), c.API.CORSOrigins...),
		TrustedProxies:          append([]string(nil), c.API.TrustedProxies...),
		ProxyHeader:             c.API.ProxyHeader,
		UnblockHTTPAPI:          c.API.PeriodicPolls,
		NoStartupMessage:        c.Mode.NoStartupMessage,
		APIToken:                c.API.Token,
		APIEventsToken:          c.API.EventsToken,
		APIHost:                 c.API.Host,
		APIHostChanged:          c.API.HostChanged,
		APIPort:                 apiPort,
		APIPortChanged:          c.API.PortChanged,
		APIRateLimit:            c.API.RateLimit,
		APIRateLimitChanged:     c.API.RateLimitChanged,
		CheckAPITimeout:         c.API.CheckTimeout,
		CheckAPITimeoutChanged:  c.API.CheckTimeoutChanged,
		UpdateAPITimeout:        c.API.UpdateTimeout,
		UpdateAPITimeoutChanged: c.API.UpdateTimeoutChanged,
	}

	// Map resolved endpoint names onto Enable* fields on RunConfig.
	setEndpointConfig(endpointSet, &cfg)

	return cfg, nil
}

// StartupParams builds logging.StartupParams from Config and RunConfig endpoint state.
//
// Runtime fields such as Sched, Filtering, Client, Notifier, and Version are left for
// callers to fill when invoking WriteStartupMessage.
//
// Parameters:
//   - run: RunConfig with resolved endpoint enablement.
//
// Returns:
//   - logging.StartupParams: Mode and HTTP API values for startup messaging.
func (c Config) StartupParams(run RunConfig) logging.StartupParams {
	return logging.StartupParams{
		NoStartupMessage:     c.Mode.NoStartupMessage,
		RunOnce:              c.Mode.RunOnce,
		HTTPAPIUpdate:        run.EnableUpdateAPI,
		HTTPAPIPeriodicPolls: run.UnblockHTTPAPI,
		DiskSpaceMaxBytes:    c.Update.DiskSpaceMaxBytes,
		DiskSpaceWarnBytes:   c.Update.DiskSpaceWarnBytes,
	}
}

// AnyHTTPAPIConfig reports whether any HTTP API-related settings are present
// without enabled endpoints, so operators can be warned about a no-op config.
func AnyHTTPAPIConfig(cfg RunConfig) bool {
	return cfg.APIToken != "" ||
		cfg.APIEventsToken != "" ||
		cfg.TLSCertPath != "" ||
		cfg.TLSKeyPath != "" ||
		len(cfg.CORSAllowedOrigins) > 0 ||
		len(cfg.TrustedProxies) > 0 ||
		cfg.ProxyHeader != "" ||
		cfg.APIHostChanged ||
		cfg.APIPortChanged ||
		cfg.APIRateLimitChanged ||
		cfg.CheckAPITimeoutChanged ||
		cfg.UpdateAPITimeoutChanged
}

// HTTPAPIEndpointsEnabled reports whether any HTTP API endpoint is enabled.
func HTTPAPIEndpointsEnabled(cfg RunConfig) bool {
	return cfg.EnableUpdateAPI ||
		cfg.EnableMetricsAPI ||
		cfg.EnableContainersAPI ||
		cfg.EnableCheckAPI ||
		cfg.EnableSwaggerAPI ||
		cfg.EnableHealthAPI ||
		cfg.EnableHistoryAPI ||
		cfg.EnableImagesAPI ||
		cfg.EnableConfigAPI ||
		cfg.EnableEventsAPI
}

// ValidateAPIHost ensures http-api-host is empty (all interfaces) or a valid IP.
//
// Parameters:
//   - host: Value of the http-api-host setting.
//
// Returns:
//   - error: Non-nil when host is a non-empty non-IP string (for example a hostname).
func ValidateAPIHost(host string) error {
	if host == "" {
		return nil
	}

	if net.ParseIP(host) == nil {
		return fmt.Errorf("%w: %q", errInvalidAPIHost, host)
	}

	return nil
}

// setEndpointConfig populates the runtime configuration with the HTTP API
// Endpoints parsed from the http-api-endpoints configuration option.
//
// Parameters:
//   - endpointMap: Enabled endpoints.
//   - cfg: RunConfig to update.
func setEndpointConfig(endpointMap api.EnabledEndpoints, cfg *RunConfig) {
	cfg.EnableHealthAPI = endpointMap.Contains(api.EndpointHealth)
	cfg.EnableUpdateAPI = endpointMap.Contains(api.EndpointUpdate)
	cfg.EnableMetricsAPI = endpointMap.Contains(api.EndpointMetrics)
	cfg.EnableContainersAPI = endpointMap.Contains(api.EndpointContainers)
	cfg.EnableCheckAPI = endpointMap.Contains(api.EndpointCheck)
	cfg.EnableHistoryAPI = endpointMap.Contains(api.EndpointHistory)
	cfg.EnableImagesAPI = endpointMap.Contains(api.EndpointImages)
	cfg.EnableConfigAPI = endpointMap.Contains(api.EndpointConfig)
	cfg.EnableEventsAPI = endpointMap.Contains(api.EndpointEvents)
	cfg.EnableSwaggerAPI = endpointMap.Contains(api.EndpointSwagger)
}
