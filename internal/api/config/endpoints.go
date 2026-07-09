package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
)

// Canonical HTTP API endpoint names for --http-api-endpoints.
const (
	EndpointHealth     = "health"
	EndpointUpdate     = "update"
	EndpointMetrics    = "metrics"
	EndpointContainers = "containers"
	EndpointCheck      = "check"
	EndpointHistory    = "history"
	EndpointImages     = "images"
	EndpointConfig     = "config"
	EndpointEvents     = "events"
	EndpointSwagger    = "swagger"
)

// AllEndpointNames is the ordered list of every known endpoint name.
var AllEndpointNames = []string{
	EndpointHealth,
	EndpointUpdate,
	EndpointMetrics,
	EndpointContainers,
	EndpointCheck,
	EndpointHistory,
	EndpointImages,
	EndpointConfig,
	EndpointEvents,
	EndpointSwagger,
}

// EndpointSet is the set of enabled HTTP API endpoint names.
type EndpointSet map[string]struct{}

// Has reports whether name is enabled.
func (s EndpointSet) Has(name string) bool {
	if s == nil {
		return false
	}

	_, ok := s[name]

	return ok
}

// Empty reports whether no endpoints are enabled.
func (s EndpointSet) Empty() bool {
	return len(s) == 0
}

var (
	// ErrUnknownEndpoint is returned when an endpoint name is not recognized.
	ErrUnknownEndpoint = errors.New("unknown HTTP API endpoint")
	// ErrAllMustBeAlone is returned when "all" appears with other names.
	ErrAllMustBeAlone = errors.New(`"all" must be the only value in --http-api-endpoints`)
)

// ParseAPIEndpoints parses an endpoint allowlist.
//
// Values may already be split on commas or spaces by flag/env parsing. Each
// entry is still trimmed and lowercased. The special value "all" (alone)
// expands to every known endpoint. Duplicates are ignored. An empty list
// yields an empty set.
//
// Parameters:
//   - values: Endpoint names, or a single "all".
//
// Returns:
//   - EndpointSet: Parsed set of endpoint names.
//   - error: Non-nil if the value is invalid.
func ParseAPIEndpoints(values []string) (EndpointSet, error) {
	names := make([]string, 0, len(values))

	for _, part := range values {
		// Allow a single string that still contains separators (e.g. tests or
		// callers that have not pre-split).
		for _, token := range strings.FieldsFunc(part, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		}) {
			name := strings.ToLower(strings.TrimSpace(token))
			if name == "" {
				continue
			}

			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return EndpointSet{}, nil
	}

	if slices.Contains(names, "all") {
		if len(names) != 1 {
			return nil, ErrAllMustBeAlone
		}

		return allEndpoints(), nil
	}

	set := make(EndpointSet, len(names))
	valid := allEndpoints()

	for _, name := range names {
		if !valid.Has(name) {
			return nil, fmt.Errorf("%w: %q (valid: %s)", ErrUnknownEndpoint, name, FormatEndpoints(valid))
		}

		set[name] = struct{}{}
	}

	return set, nil
}

// EndpointsFromLegacyMainFlags builds an endpoint set from origin/main legacy
// enable flags (update, metrics, containers only).
//
// Deprecated: Prefer ParseAPIEndpoints with --http-api-endpoints. Legacy flags
// will be removed in the v2 release.
//
// Parameters:
//   - update: Whether --http-api-update is set.
//   - metrics: Whether --http-api-metrics is set.
//   - containers: Whether --http-api-containers is set.
//
// Returns:
//   - EndpointSet: Mapped endpoint names.
//
// TODO: Remove EndpointsFromLegacyMainFlags when legacy HTTP API flags are removed in v2.
//
//nolint:godox
func EndpointsFromLegacyMainFlags(update, metrics, containers bool) EndpointSet {
	set := make(EndpointSet)

	if update {
		set[EndpointUpdate] = struct{}{}
	}

	if metrics {
		set[EndpointMetrics] = struct{}{}
	}

	if containers {
		set[EndpointContainers] = struct{}{}
	}

	return set
}

// FormatEndpoints returns a stable comma-separated list of endpoint names
// in AllEndpointNames order (only those present in set).
//
// Parameters:
//   - set: Endpoint set to format.
//
// Returns:
//   - string: Comma-separated names, or empty if set is empty.
func FormatEndpoints(set EndpointSet) string {
	if set.Empty() {
		return ""
	}

	parts := make([]string, 0, len(set))
	for _, name := range AllEndpointNames {
		if set.Has(name) {
			parts = append(parts, name)
		}
	}

	// Include any unexpected keys for debugging (should not happen).
	for name := range set {
		if !slices.Contains(AllEndpointNames, name) {
			parts = append(parts, name)
		}
	}

	return strings.Join(parts, ",")
}

// ResolveEndpoints selects the active endpoint set from the canonical allowlist
// and/or legacy main flags.
//
// Rules:
//  1. Parse the allowlist when non-empty (unknown names fail; "all" expands fully).
//  2. Union with any legacy update/metrics/containers flags (deduplicated).
//  3. When legacy flags are used, log one deprecation warning with the final
//     equivalent allowlist value.
//  4. Empty allowlist and no legacy flags → empty set (API off).
//
// Parameters:
//   - endpoints: Values of --http-api-endpoints / WATCHTOWER_HTTP_API_ENDPOINTS
//     (already split on commas/spaces when set via flags).
//   - legacyUpdate: Legacy --http-api-update.
//   - legacyMetrics: Legacy --http-api-metrics.
//   - legacyContainers: Legacy --http-api-containers.
//
// Returns:
//   - EndpointSet: Resolved enabled endpoints.
//   - error: Non-nil if the allowlist contains invalid values.
//
// TODO: Drop legacy flag parameters when removing legacy HTTP API flags in v2.
//
//nolint:godox
func ResolveEndpoints(endpoints []string, legacyUpdate, legacyMetrics, legacyContainers bool) (EndpointSet, error) {
	legacyAny := legacyUpdate || legacyMetrics || legacyContainers

	set := EndpointSet{}

	if len(endpoints) > 0 {
		parsed, err := ParseAPIEndpoints(endpoints)
		if err != nil {
			return nil, err
		}

		set = parsed
	}

	if legacyAny {
		legacySet := EndpointsFromLegacyMainFlags(legacyUpdate, legacyMetrics, legacyContainers)
		for name := range legacySet {
			if set == nil {
				set = EndpointSet{}
			}

			set[name] = struct{}{}
		}

		// Log when legacy flags/env are used (covers env defaults that may not
		// surface pflag's Flag.Deprecated warning the same way as CLI usage).
		logrus.WithField("http_api_endpoints", FormatEndpoints(set)).Warn(
			"Deprecated: --http-api-update/--http-api-metrics/--http-api-containers are deprecated and will be removed in Watchtower v2; " +
				"use --http-api-endpoints (or WATCHTOWER_HTTP_API_ENDPOINTS) instead. " +
				"Equivalent: --http-api-endpoints=" + FormatEndpoints(set),
		)
	}

	if set == nil {
		return EndpointSet{}, nil
	}

	return set, nil
}

// ApplyEndpoints sets Enable*API fields on opts from the endpoint set.
//
// Parameters:
//   - opts: Options to update (modified in place).
//   - set: Enabled endpoints.
func ApplyEndpoints(opts *Options, set EndpointSet) {
	opts.EnableHealthAPI = set.Has(EndpointHealth)
	opts.EnableUpdateAPI = set.Has(EndpointUpdate)
	opts.EnableMetricsAPI = set.Has(EndpointMetrics)
	opts.EnableContainersAPI = set.Has(EndpointContainers)
	opts.EnableCheckAPI = set.Has(EndpointCheck)
	opts.EnableHistoryAPI = set.Has(EndpointHistory)
	opts.EnableImagesAPI = set.Has(EndpointImages)
	opts.EnableConfigAPI = set.Has(EndpointConfig)
	opts.EnableEventsAPI = set.Has(EndpointEvents)
	opts.EnableSwaggerAPI = set.Has(EndpointSwagger)
}

// ApplyEndpointsToBools mirrors ApplyEndpoints for types that only carry
// the Enable* bool fields (used from cmd).
//
// Parameters:
//   - set: Enabled endpoints.
//
// Returns:
//   - Ten booleans in order: health, update, metrics, containers, check,
//     history, images, config, events, swagger.
func ApplyEndpointsToBools(set EndpointSet) (bool, bool, bool, bool, bool, bool, bool, bool, bool, bool) {
	return set.Has(EndpointHealth),
		set.Has(EndpointUpdate),
		set.Has(EndpointMetrics),
		set.Has(EndpointContainers),
		set.Has(EndpointCheck),
		set.Has(EndpointHistory),
		set.Has(EndpointImages),
		set.Has(EndpointConfig),
		set.Has(EndpointEvents),
		set.Has(EndpointSwagger)
}

func allEndpoints() EndpointSet {
	set := make(EndpointSet, len(AllEndpointNames))
	for _, name := range AllEndpointNames {
		set[name] = struct{}{}
	}

	return set
}
