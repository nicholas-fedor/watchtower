package api

import "time"

// API holds HTTP API server configuration.
type API struct {
	// Host is the bind host.
	Host string
	// Port is the bind port.
	Port string
	// Token is the API authentication token.
	Token string
	// EventsToken is the events SSE authentication token.
	EventsToken string
	// TLSCert is the path to the TLS certificate file.
	TLSCert string
	// TLSKey is the path to the TLS key file.
	TLSKey string
	// ProxyHeader is the header used for the real client IP.
	ProxyHeader string
	// Endpoints lists enabled endpoint names, or contains "all".
	Endpoints []string
	// TrustedProxies lists trusted proxy CIDRs or IPs.
	TrustedProxies []string
	// CORSOrigins lists allowed CORS origins.
	CORSOrigins []string
	// RateLimit is max auth requests per minute per IP.
	RateLimit int
	// CheckTimeout is the max duration for /v1/check.
	CheckTimeout time.Duration
	// UpdateTimeout is the max duration for /v1/update.
	UpdateTimeout time.Duration
	// LegacyUpdate enables the legacy http-api-update flag behavior.
	LegacyUpdate bool
	// LegacyMetrics enables the legacy http-api-metrics flag behavior.
	LegacyMetrics bool
	// LegacyContainers enables the legacy http-api-containers flag behavior.
	LegacyContainers bool
	// HostChanged is true when the host flag was explicitly set.
	HostChanged bool
	// PortChanged is true when the port flag was explicitly set.
	PortChanged bool
	// PeriodicPolls keeps scheduled polls when the HTTP API is enabled.
	PeriodicPolls bool
	// RateLimitChanged is true when the rate-limit flag was explicitly set.
	RateLimitChanged bool
	// CheckTimeoutChanged is true when the check-timeout flag was explicitly set.
	CheckTimeoutChanged bool
	// UpdateTimeoutChanged is true when the update-timeout flag was explicitly set.
	UpdateTimeoutChanged bool
}
