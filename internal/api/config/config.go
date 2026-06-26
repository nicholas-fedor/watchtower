// Package config defines the shared configuration types, validation functions,
// and sentinel errors used across the API packages. It exists to break the
// import cycle between the top-level api package and the routes subpackage.
package config

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
	mt "github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var (
	// ErrMissingRunUpdatesWithNotifications indicates RunUpdatesWithNotifications was not provided.
	ErrMissingRunUpdatesWithNotifications = errors.New("RunUpdatesWithNotifications must be provided when EnableUpdateAPI is set")
	// ErrMissingFilterByImage indicates FilterByImage was not provided.
	ErrMissingFilterByImage = errors.New("FilterByImage must be provided when EnableUpdateAPI is set")
	// ErrMissingDefaultMetrics indicates DefaultMetrics was not provided.
	ErrMissingDefaultMetrics = errors.New("DefaultMetrics must be provided when EnableUpdateAPI is set")
	// ErrMissingAPIToken indicates the API token is empty or unset.
	ErrMissingAPIToken = errors.New("API token is empty or unset")
	// ErrMissingEventsAPIToken indicates events token is not set when events API is enabled.
	ErrMissingEventsAPIToken = errors.New("events API token is required when events API is enabled")
	// ErrMissingTLSConfig indicates only one of TLS cert/key was provided.
	ErrMissingTLSConfig = errors.New("TLS requires both TLS Cert Path and TLS Key Path to be set")
)

const (
	// HandlerTimeout defines the maximum duration for non-update handlers to
	// complete. This prevents slow Docker API calls from blocking connections
	// indefinitely.
	HandlerTimeout = 30 * time.Second
)

// Options holds all configuration for SetupAndStartAPI.
type Options struct {
	Host                        string
	Port                        string
	Token                       string
	EventsToken                 string
	RateLimit                   int
	EnableUpdateAPI             bool
	EnableMetricsAPI            bool
	EnableContainersAPI         bool
	EnableCheckAPI              bool
	EnableSwaggerAPI            bool
	EnableHealthAPI             bool
	EnableHistoryAPI            bool
	EnableImagesAPI             bool
	EnableConfigAPI             bool
	EnableEventsAPI             bool
	EnableFullAPI               bool
	UnblockHTTPAPI              bool
	NoStartupMessage            bool
	TLSCertPath                 string
	TLSKeyPath                  string
	CORSAllowedOrigins          []string
	TrustedProxies              []string
	ProxyHeader                 string
	Filter                      types.Filter
	Command                     *cobra.Command
	FilterDesc                  string
	UpdateLock                  chan bool
	Cleanup                     bool
	MonitorOnly                 bool
	NoPull                      bool
	NoRestart                   bool
	RollingRestart              bool
	IncludeStopped              bool
	IncludeRestarting           bool
	LifecycleHooks              bool
	LabelEnable                 bool
	SkipSelfUpdate              bool
	Client                      container.Client
	Notifier                    types.Notifier
	Scope                       string
	Version                     string
	RunUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *mt.Metric
	FilterByImage               func([]string, types.Filter) types.Filter
	DefaultMetrics              func() *mt.Metrics
	WriteStartupMessage         func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool)
	EventBroadcaster            *events.Broadcaster
}

// ValidateUpdateOptions validates that all required update options are set.
//
// Parameters:
//   - opts: API configuration options to validate.
//
// Returns:
//   - error: Non-nil if any required option is missing.
func ValidateUpdateOptions(opts Options) error {
	// RunUpdatesWithNotifications executes the scan-and-update pipeline,
	// which is the core operation of the update endpoint.
	if opts.RunUpdatesWithNotifications == nil {
		return ErrMissingRunUpdatesWithNotifications
	}

	// FilterByImage builds an image-level predicate that the update endpoint
	// combines with container-level filters to scope which containers are
	// scanned.
	if opts.FilterByImage == nil {
		return ErrMissingFilterByImage
	}

	// DefaultMetrics provides the metrics store where the update endpoint
	// records scan results after each run.
	if opts.DefaultMetrics == nil {
		return ErrMissingDefaultMetrics
	}

	return nil
}

// TimeoutMiddleware returns a Fiber middleware that enforces a per-request
// timeout for all wrapped handlers. This prevents slow Docker API calls from
// blocking connections indefinitely.
func TimeoutMiddleware() fiber.Handler {
	return timeout.New(func(c fiber.Ctx) error {
		return c.Next()
	}, timeout.Config{
		Timeout: HandlerTimeout,
	})
}
