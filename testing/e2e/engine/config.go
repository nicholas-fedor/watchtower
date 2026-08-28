package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// WatchtowerConfig is every Watchtower flag domain. Nil / unset means default.
type WatchtowerConfig struct {
	Host       *string
	TLSVerify  *bool
	APIVersion *string
	CertPath   *string

	IncludeStopped    *bool
	IncludeRestarting *bool
	ReviveStopped     *bool
	RemoveVolumes     *bool
	WarnOnHeadFailure *string

	Interval      *int
	Schedule      *string
	UpdateOnStart *bool

	RunOnce                *bool
	HealthCheck            *bool
	Porcelain              *string
	NoStartupMessage       *bool
	SelfUpdateOrchestrator *bool

	Cleanup             *bool
	NoPull              *bool
	NoRestart           *bool
	MonitorOnly         *bool
	RollingRestart      *bool
	StopTimeout         *time.Duration
	CooldownDelay       *string
	UseComposeDependsOn *bool
	LabelTakePrecedence *bool
	EphemeralSelfUpdate *bool
	DiskSpaceMax        *string
	DiskSpaceWarn       *string

	EnableLifecycleHooks *bool
	LifecycleUID         *int
	LifecycleGID         *int

	LabelEnable              *bool
	DisableContainers        *[]string
	MonitorImageNames        *[]string
	SkipImageNames           *[]string
	EnableContainersByLabel  *[]string
	DisableContainersByLabel *[]string
	Scope                    *string

	RegistryTLSSkip       *bool
	RegistryTLSMinVersion *string

	DisableMemorySwappiness *bool
	CPUCopyMode             *string

	HTTPAPIEndpoints      *[]string
	HTTPAPIUpdate         *bool
	HTTPAPIMetrics        *bool
	HTTPAPIContainers     *bool
	HTTPAPIHost           *string
	HTTPAPIPort           *string
	HTTPAPIToken          *string
	HTTPAPIEventsToken    *string
	HTTPAPIPeriodicPolls  *bool
	HTTPAPIRateLimit      *int
	HTTPAPITLSCert        *string
	HTTPAPITLSKey         *string
	HTTPAPITrustedProxies *[]string
	HTTPAPIProxyHeader    *string
	HTTPAPICORSOrigins    *[]string
	HTTPAPICheckTimeout   *time.Duration
	HTTPAPIUpdateTimeout  *time.Duration

	NotificationURL              *[]string
	NotificationsLevel           *string
	NotificationsDelay           *int
	NotificationsHostname        *string
	NotificationTemplate         *string
	NotificationTemplateFile     *string
	NotificationReport           *bool
	NotificationTitleTag         *string
	NotificationSkipTitle        *bool
	NotificationLogStdout        *bool
	NotificationSplitByContainer *bool
	Notifications                *[]string
	NotificationEmailFrom        *string
	NotificationEmailTo          *string
	NotificationEmailDelay       *int
	NotificationEmailServer      *string
	NotificationEmailServerPort  *int
	NotificationEmailTLSSkip     *bool
	NotificationEmailUser        *string
	NotificationEmailPassword    *string
	NotificationEmailSubjectTag  *string
	NotificationSlackHookURL     *string
	NotificationSlackIdentifier  *string
	NotificationSlackChannel     *string
	NotificationSlackIconEmoji   *string
	NotificationSlackIconURL     *string
	NotificationMSTeamsHook      *string
	NotificationGotifyURL        *string
	NotificationGotifyToken      *string
	NotificationGotifyTLSSkip    *bool

	LogFormat *string
	LogLevel  *string
	Debug     *bool
	Trace     *bool
	NoColor   *bool
}

// flagSpec maps one WatchtowerConfig field onto CLI and environment names.
type flagSpec struct {
	// flag is the long CLI name without dashes.
	flag string
	// env is the environment key, or empty when the flag has no env binding.
	env string
	// kind selects argv rendering.
	kind renderKind
	// get reads the field. set is false when the pointer is nil.
	get func(WatchtowerConfig) (set bool, value string)
}

// renderKind selects how a field is written to argv and env.
type renderKind int

const (
	// renderBool is a boolean flag.
	renderBool renderKind = iota
	// renderScalar is a string, int, or duration.
	renderScalar
	// renderSlice is a comma-joined list.
	renderSlice
)

// specs returns the render table covering every RegisterAll flag.
//
// Returns:
//   - []flagSpec: Flag/env renderers in registration order.
func specs() []flagSpec {
	return []flagSpec{
		strSpec("host", "DOCKER_HOST", func(c WatchtowerConfig) *string { return c.Host }),
		boolSpec("tlsverify", "DOCKER_TLS_VERIFY", func(c WatchtowerConfig) *bool { return c.TLSVerify }),
		strSpec("api-version", "DOCKER_API_VERSION", func(c WatchtowerConfig) *string { return c.APIVersion }),
		strSpec("cert-path", "DOCKER_CERT_PATH", func(c WatchtowerConfig) *string { return c.CertPath }),

		boolSpec("include-stopped", "WATCHTOWER_INCLUDE_STOPPED", func(c WatchtowerConfig) *bool { return c.IncludeStopped }),
		boolSpec("include-restarting", "WATCHTOWER_INCLUDE_RESTARTING", func(c WatchtowerConfig) *bool { return c.IncludeRestarting }),
		boolSpec("revive-stopped", "WATCHTOWER_REVIVE_STOPPED", func(c WatchtowerConfig) *bool { return c.ReviveStopped }),
		boolSpec("remove-volumes", "WATCHTOWER_REMOVE_VOLUMES", func(c WatchtowerConfig) *bool { return c.RemoveVolumes }),
		strSpec("warn-on-head-failure", "WATCHTOWER_WARN_ON_HEAD_FAILURE", func(c WatchtowerConfig) *string { return c.WarnOnHeadFailure }),

		intSpec("interval", "WATCHTOWER_POLL_INTERVAL", func(c WatchtowerConfig) *int { return c.Interval }),
		strSpec("schedule", "WATCHTOWER_SCHEDULE", func(c WatchtowerConfig) *string { return c.Schedule }),
		boolSpec("update-on-start", "WATCHTOWER_UPDATE_ON_START", func(c WatchtowerConfig) *bool { return c.UpdateOnStart }),

		boolSpec("run-once", "WATCHTOWER_RUN_ONCE", func(c WatchtowerConfig) *bool { return c.RunOnce }),
		boolSpec("health-check", "", func(c WatchtowerConfig) *bool { return c.HealthCheck }),
		strSpec("porcelain", "WATCHTOWER_PORCELAIN", func(c WatchtowerConfig) *string { return c.Porcelain }),
		boolSpec("no-startup-message", "WATCHTOWER_NO_STARTUP_MESSAGE", func(c WatchtowerConfig) *bool { return c.NoStartupMessage }),
		boolSpec("self-update-orchestrator", "", func(c WatchtowerConfig) *bool { return c.SelfUpdateOrchestrator }),

		boolSpec("cleanup", "WATCHTOWER_CLEANUP", func(c WatchtowerConfig) *bool { return c.Cleanup }),
		boolSpec("no-pull", "WATCHTOWER_NO_PULL", func(c WatchtowerConfig) *bool { return c.NoPull }),
		boolSpec("no-restart", "WATCHTOWER_NO_RESTART", func(c WatchtowerConfig) *bool { return c.NoRestart }),
		boolSpec("monitor-only", "WATCHTOWER_MONITOR_ONLY", func(c WatchtowerConfig) *bool { return c.MonitorOnly }),
		boolSpec("rolling-restart", "WATCHTOWER_ROLLING_RESTART", func(c WatchtowerConfig) *bool { return c.RollingRestart }),
		durSpec("stop-timeout", "WATCHTOWER_TIMEOUT", func(c WatchtowerConfig) *time.Duration { return c.StopTimeout }),
		strSpec("cooldown-delay", "WATCHTOWER_COOLDOWN_DELAY", func(c WatchtowerConfig) *string { return c.CooldownDelay }),
		boolSpec("use-compose-depends-on", "WATCHTOWER_USE_COMPOSE_DEPENDS_ON", func(c WatchtowerConfig) *bool { return c.UseComposeDependsOn }),
		boolSpec("label-take-precedence", "WATCHTOWER_LABEL_TAKE_PRECEDENCE", func(c WatchtowerConfig) *bool { return c.LabelTakePrecedence }),
		boolSpec("ephemeral-self-update", "WATCHTOWER_EPHEMERAL_SELF_UPDATE", func(c WatchtowerConfig) *bool { return c.EphemeralSelfUpdate }),
		strSpec("disk-space-max", "WATCHTOWER_DISK_SPACE_MAX", func(c WatchtowerConfig) *string { return c.DiskSpaceMax }),
		strSpec("disk-space-warn", "WATCHTOWER_DISK_SPACE_WARN", func(c WatchtowerConfig) *string { return c.DiskSpaceWarn }),

		boolSpec("enable-lifecycle-hooks", "WATCHTOWER_LIFECYCLE_HOOKS", func(c WatchtowerConfig) *bool { return c.EnableLifecycleHooks }),
		intSpec("lifecycle-uid", "WATCHTOWER_LIFECYCLE_UID", func(c WatchtowerConfig) *int { return c.LifecycleUID }),
		intSpec("lifecycle-gid", "WATCHTOWER_LIFECYCLE_GID", func(c WatchtowerConfig) *int { return c.LifecycleGID }),

		boolSpec("label-enable", "WATCHTOWER_LABEL_ENABLE", func(c WatchtowerConfig) *bool { return c.LabelEnable }),
		sliceSpec("disable-containers", "WATCHTOWER_DISABLE_CONTAINERS", func(c WatchtowerConfig) *[]string { return c.DisableContainers }),
		sliceSpec("monitor-image-names", "WATCHTOWER_MONITOR_IMAGE_NAMES", func(c WatchtowerConfig) *[]string { return c.MonitorImageNames }),
		sliceSpec("skip-image-names", "WATCHTOWER_SKIP_IMAGE_NAMES", func(c WatchtowerConfig) *[]string { return c.SkipImageNames }),
		sliceSpec("enable-containers-by-label", "WATCHTOWER_ENABLE_CONTAINERS_BY_LABEL", func(c WatchtowerConfig) *[]string { return c.EnableContainersByLabel }),
		sliceSpec("disable-containers-by-label", "WATCHTOWER_DISABLE_CONTAINERS_BY_LABEL", func(c WatchtowerConfig) *[]string { return c.DisableContainersByLabel }),
		strSpec("scope", "WATCHTOWER_SCOPE", func(c WatchtowerConfig) *string { return c.Scope }),

		boolSpec("registry-tls-skip", "WATCHTOWER_REGISTRY_TLS_SKIP", func(c WatchtowerConfig) *bool { return c.RegistryTLSSkip }),
		strSpec("registry-tls-min-version", "WATCHTOWER_REGISTRY_TLS_MIN_VERSION", func(c WatchtowerConfig) *string { return c.RegistryTLSMinVersion }),

		boolSpec("disable-memory-swappiness", "WATCHTOWER_DISABLE_MEMORY_SWAPPINESS", func(c WatchtowerConfig) *bool { return c.DisableMemorySwappiness }),
		strSpec("cpu-copy-mode", "WATCHTOWER_CPU_COPY_MODE", func(c WatchtowerConfig) *string { return c.CPUCopyMode }),

		sliceSpec("http-api-endpoints", "WATCHTOWER_HTTP_API_ENDPOINTS", func(c WatchtowerConfig) *[]string { return c.HTTPAPIEndpoints }),
		boolSpec("http-api-update", "WATCHTOWER_HTTP_API_UPDATE", func(c WatchtowerConfig) *bool { return c.HTTPAPIUpdate }),
		boolSpec("http-api-metrics", "WATCHTOWER_HTTP_API_METRICS", func(c WatchtowerConfig) *bool { return c.HTTPAPIMetrics }),
		boolSpec("http-api-containers", "WATCHTOWER_HTTP_API_CONTAINERS", func(c WatchtowerConfig) *bool { return c.HTTPAPIContainers }),
		strSpec("http-api-host", "WATCHTOWER_HTTP_API_HOST", func(c WatchtowerConfig) *string { return c.HTTPAPIHost }),
		strSpec("http-api-port", "WATCHTOWER_HTTP_API_PORT", func(c WatchtowerConfig) *string { return c.HTTPAPIPort }),
		strSpec("http-api-token", "WATCHTOWER_HTTP_API_TOKEN", func(c WatchtowerConfig) *string { return c.HTTPAPIToken }),
		strSpec("http-api-events-token", "WATCHTOWER_HTTP_API_EVENTS_TOKEN", func(c WatchtowerConfig) *string { return c.HTTPAPIEventsToken }),
		boolSpec("http-api-periodic-polls", "WATCHTOWER_HTTP_API_PERIODIC_POLLS", func(c WatchtowerConfig) *bool { return c.HTTPAPIPeriodicPolls }),
		intSpec("http-api-rate-limit", "WATCHTOWER_HTTP_API_RATE_LIMIT", func(c WatchtowerConfig) *int { return c.HTTPAPIRateLimit }),
		strSpec("http-api-tls-cert", "WATCHTOWER_HTTP_API_TLS_CERT", func(c WatchtowerConfig) *string { return c.HTTPAPITLSCert }),
		strSpec("http-api-tls-key", "WATCHTOWER_HTTP_API_TLS_KEY", func(c WatchtowerConfig) *string { return c.HTTPAPITLSKey }),
		sliceSpec("http-api-trusted-proxies", "WATCHTOWER_HTTP_API_TRUSTED_PROXIES", func(c WatchtowerConfig) *[]string { return c.HTTPAPITrustedProxies }),
		strSpec("http-api-proxy-header", "WATCHTOWER_HTTP_API_PROXY_HEADER", func(c WatchtowerConfig) *string { return c.HTTPAPIProxyHeader }),
		sliceSpec("http-api-cors-origins", "WATCHTOWER_HTTP_API_CORS_ORIGINS", func(c WatchtowerConfig) *[]string { return c.HTTPAPICORSOrigins }),
		durSpec("http-api-check-timeout", "WATCHTOWER_HTTP_API_CHECK_TIMEOUT", func(c WatchtowerConfig) *time.Duration { return c.HTTPAPICheckTimeout }),
		durSpec("http-api-update-timeout", "WATCHTOWER_HTTP_API_UPDATE_TIMEOUT", func(c WatchtowerConfig) *time.Duration { return c.HTTPAPIUpdateTimeout }),

		sliceSpec("notification-url", "WATCHTOWER_NOTIFICATION_URL", func(c WatchtowerConfig) *[]string { return c.NotificationURL }),
		strSpec("notifications-level", "WATCHTOWER_NOTIFICATIONS_LEVEL", func(c WatchtowerConfig) *string { return c.NotificationsLevel }),
		intSpec("notifications-delay", "WATCHTOWER_NOTIFICATIONS_DELAY", func(c WatchtowerConfig) *int { return c.NotificationsDelay }),
		strSpec("notifications-hostname", "WATCHTOWER_NOTIFICATIONS_HOSTNAME", func(c WatchtowerConfig) *string { return c.NotificationsHostname }),
		strSpec("notification-template", "WATCHTOWER_NOTIFICATION_TEMPLATE", func(c WatchtowerConfig) *string { return c.NotificationTemplate }),
		strSpec("notification-template-file", "WATCHTOWER_NOTIFICATION_TEMPLATE_FILE", func(c WatchtowerConfig) *string { return c.NotificationTemplateFile }),
		boolSpec("notification-report", "WATCHTOWER_NOTIFICATION_REPORT", func(c WatchtowerConfig) *bool { return c.NotificationReport }),
		strSpec("notification-title-tag", "WATCHTOWER_NOTIFICATION_TITLE_TAG", func(c WatchtowerConfig) *string { return c.NotificationTitleTag }),
		boolSpec("notification-skip-title", "WATCHTOWER_NOTIFICATION_SKIP_TITLE", func(c WatchtowerConfig) *bool { return c.NotificationSkipTitle }),
		boolSpec("notification-log-stdout", "WATCHTOWER_NOTIFICATION_LOG_STDOUT", func(c WatchtowerConfig) *bool { return c.NotificationLogStdout }),
		boolSpec("notification-split-by-container", "WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER", func(c WatchtowerConfig) *bool { return c.NotificationSplitByContainer }),
		sliceSpec("notifications", "WATCHTOWER_NOTIFICATIONS", func(c WatchtowerConfig) *[]string { return c.Notifications }),
		strSpec("notification-email-from", "WATCHTOWER_NOTIFICATION_EMAIL_FROM", func(c WatchtowerConfig) *string { return c.NotificationEmailFrom }),
		strSpec("notification-email-to", "WATCHTOWER_NOTIFICATION_EMAIL_TO", func(c WatchtowerConfig) *string { return c.NotificationEmailTo }),
		intSpec("notification-email-delay", "WATCHTOWER_NOTIFICATION_EMAIL_DELAY", func(c WatchtowerConfig) *int { return c.NotificationEmailDelay }),
		strSpec("notification-email-server", "WATCHTOWER_NOTIFICATION_EMAIL_SERVER", func(c WatchtowerConfig) *string { return c.NotificationEmailServer }),
		intSpec("notification-email-server-port", "WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT", func(c WatchtowerConfig) *int { return c.NotificationEmailServerPort }),
		boolSpec("notification-email-server-tls-skip-verify", "WATCHTOWER_NOTIFICATION_EMAIL_SERVER_TLS_SKIP_VERIFY", func(c WatchtowerConfig) *bool { return c.NotificationEmailTLSSkip }),
		strSpec("notification-email-server-user", "WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER", func(c WatchtowerConfig) *string { return c.NotificationEmailUser }),
		strSpec("notification-email-server-password", "WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD", func(c WatchtowerConfig) *string { return c.NotificationEmailPassword }),
		strSpec("notification-email-subjecttag", "WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG", func(c WatchtowerConfig) *string { return c.NotificationEmailSubjectTag }),
		strSpec("notification-slack-hook-url", "WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL", func(c WatchtowerConfig) *string { return c.NotificationSlackHookURL }),
		strSpec("notification-slack-identifier", "WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER", func(c WatchtowerConfig) *string { return c.NotificationSlackIdentifier }),
		strSpec("notification-slack-channel", "WATCHTOWER_NOTIFICATION_SLACK_CHANNEL", func(c WatchtowerConfig) *string { return c.NotificationSlackChannel }),
		strSpec("notification-slack-icon-emoji", "WATCHTOWER_NOTIFICATION_SLACK_ICON_EMOJI", func(c WatchtowerConfig) *string { return c.NotificationSlackIconEmoji }),
		strSpec("notification-slack-icon-url", "WATCHTOWER_NOTIFICATION_SLACK_ICON_URL", func(c WatchtowerConfig) *string { return c.NotificationSlackIconURL }),
		strSpec("notification-msteams-hook", "WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL", func(c WatchtowerConfig) *string { return c.NotificationMSTeamsHook }),
		strSpec("notification-gotify-url", "WATCHTOWER_NOTIFICATION_GOTIFY_URL", func(c WatchtowerConfig) *string { return c.NotificationGotifyURL }),
		strSpec("notification-gotify-token", "WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN", func(c WatchtowerConfig) *string { return c.NotificationGotifyToken }),
		boolSpec("notification-gotify-tls-skip-verify", "WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY", func(c WatchtowerConfig) *bool { return c.NotificationGotifyTLSSkip }),

		strSpec("log-format", "WATCHTOWER_LOG_FORMAT", func(c WatchtowerConfig) *string { return c.LogFormat }),
		strSpec("log-level", "WATCHTOWER_LOG_LEVEL", func(c WatchtowerConfig) *string { return c.LogLevel }),
		boolSpec("debug", "WATCHTOWER_DEBUG", func(c WatchtowerConfig) *bool { return c.Debug }),
		boolSpec("trace", "WATCHTOWER_TRACE", func(c WatchtowerConfig) *bool { return c.Trace }),
		boolSpec("no-color", "NO_COLOR", func(c WatchtowerConfig) *bool { return c.NoColor }),
	}
}

// boolSpec renders a boolean Watchtower field as a CLI flag and environment variable.
//
// Parameters:
//   - flag: Long flag name.
//   - env: Environment key, or empty when the flag has no env binding.
//   - getter: Reads the optional bool from the config.
//
// Returns:
//   - flagSpec: Renderer for that field.
func boolSpec(flag, env string, getter func(WatchtowerConfig) *bool) flagSpec {
	return flagSpec{
		flag: flag,
		env:  env,
		kind: renderBool,
		get: func(cfg WatchtowerConfig) (bool, string) {
			ptr := getter(cfg)
			if ptr == nil {
				return false, ""
			}

			if *ptr {
				return true, "true"
			}

			return true, "false"
		},
	}
}

// strSpec renders a string Watchtower field as a CLI flag and environment variable.
//
// Parameters:
//   - flag: Long flag name.
//   - env: Environment key.
//   - getter: Reads the optional string from the config.
//
// Returns:
//   - flagSpec: Renderer for that field.
func strSpec(flag, env string, getter func(WatchtowerConfig) *string) flagSpec {
	return flagSpec{
		flag: flag,
		env:  env,
		kind: renderScalar,
		get: func(cfg WatchtowerConfig) (bool, string) {
			ptr := getter(cfg)
			if ptr == nil {
				return false, ""
			}

			return true, *ptr
		},
	}
}

// intSpec renders an integer Watchtower field as a CLI flag and environment variable.
//
// Parameters:
//   - flag: Long flag name.
//   - env: Environment key.
//   - getter: Reads the optional int from the config.
//
// Returns:
//   - flagSpec: Renderer for that field.
func intSpec(flag, env string, getter func(WatchtowerConfig) *int) flagSpec {
	return flagSpec{
		flag: flag,
		env:  env,
		kind: renderScalar,
		get: func(cfg WatchtowerConfig) (bool, string) {
			ptr := getter(cfg)
			if ptr == nil {
				return false, ""
			}

			return true, strconv.Itoa(*ptr)
		},
	}
}

// durSpec renders a duration Watchtower field as a CLI flag and environment variable.
//
// Parameters:
//   - flag: Long flag name.
//   - env: Environment key.
//   - getter: Reads the optional duration from the config.
//
// Returns:
//   - flagSpec: Renderer for that field.
func durSpec(flag, env string, getter func(WatchtowerConfig) *time.Duration) flagSpec {
	return flagSpec{
		flag: flag,
		env:  env,
		kind: renderScalar,
		get: func(cfg WatchtowerConfig) (bool, string) {
			ptr := getter(cfg)
			if ptr == nil {
				return false, ""
			}

			return true, ptr.String()
		},
	}
}

// sliceSpec renders a string-slice Watchtower field as a comma-joined value.
//
// Parameters:
//   - flag: Long flag name.
//   - env: Environment key.
//   - getter: Reads the optional slice from the config.
//
// Returns:
//   - flagSpec: Renderer for that field.
func sliceSpec(flag, env string, getter func(WatchtowerConfig) *[]string) flagSpec {
	return flagSpec{
		flag: flag,
		env:  env,
		kind: renderSlice,
		get: func(cfg WatchtowerConfig) (bool, string) {
			ptr := getter(cfg)
			if ptr == nil {
				return false, ""
			}

			return true, strings.Join(*ptr, ",")
		},
	}
}

// FlagNames returns every Watchtower long flag the engine can render.
//
// Returns:
//   - []string: Long flag names in registration order.
func FlagNames() []string {
	table := specs()

	names := make([]string, 0, len(table))
	for _, spec := range table {
		names = append(names, spec.flag)
	}

	return names
}

// Args renders set fields as CLI arguments. Unset fields are omitted.
//
// Returns:
//   - []string: Argv fragment (no binary name).
func (c *WatchtowerConfig) Args() []string {
	args := make([]string, 0)

	for _, spec := range specs() {
		set, value := spec.get(*c)
		if !set {
			continue
		}

		switch spec.kind {
		case renderBool:
			if value == "true" {
				args = append(args, "--"+spec.flag)

				continue
			}

			args = append(args, "--"+spec.flag+"=false")
		case renderSlice:
			args = append(args, "--"+spec.flag, value)
		case renderScalar:
			args = append(args, "--"+spec.flag, value)
		}
	}

	return args
}

// Env renders set fields as environment variables. Unset fields are omitted.
//
// Returns:
//   - map[string]string: Environment map using Watchtower / Docker env keys.
func (c *WatchtowerConfig) Env() map[string]string {
	env := make(map[string]string)
	for _, spec := range specs() {
		if spec.env == "" {
			continue
		}

		set, value := spec.get(*c)
		if !set {
			continue
		}

		env[spec.env] = value
	}

	return env
}

// Render returns argv and env according to the config channel.
//
// Mixed mode partitions by flag name: even-hash fields go to argv, odd to env.
// Secret-file mode leaves token-like fields as paths for the runner to fill.
//
// Parameters:
//   - channel: How to deliver the vector.
//
// Returns:
//   - []string: CLI arguments (possibly empty).
//   - map[string]string: Environment map (possibly empty).
func (c *WatchtowerConfig) Render(channel ConfigChannel) ([]string, map[string]string) {
	switch channel {
	case ChannelEnv, ChannelSecretFile:
		return nil, c.Env()
	case ChannelMixed:
		return c.renderMixed()
	case ChannelFlags:
		return c.Args(), nil
	default:
		return c.Args(), nil
	}
}

// ApplyObservability sets trace JSON logging unless the case already tests logs.
//
// This is harness instrumentation, not a Watchtower-under-test dimension.
func (c *WatchtowerConfig) ApplyObservability() {
	if c.LogFormat == nil {
		c.LogFormat = new("json")
	}

	if c.LogLevel == nil && (c.Debug == nil || !*c.Debug) && (c.Trace == nil || !*c.Trace) {
		c.LogLevel = new("trace")
	}

	if c.NoColor == nil {
		c.NoColor = new(true)
	}
}

// SecretValues returns configured tokens and passwords for leak assertions.
//
// Returns:
//   - []string: Non-empty secret strings from the vector.
func (c *WatchtowerConfig) SecretValues() []string {
	secrets := make([]string, 0)
	add := func(ptr *string) {
		if ptr != nil && *ptr != "" {
			secrets = append(secrets, *ptr)
		}
	}

	add(c.HTTPAPIToken)
	add(c.HTTPAPIEventsToken)
	add(c.NotificationEmailPassword)
	add(c.NotificationGotifyToken)
	add(c.NotificationSlackHookURL)

	if c.NotificationURL != nil {
		secrets = append(secrets, *c.NotificationURL...)
	}

	return secrets
}

// renderMixed splits fields across argv and env with a stable hash partition.
//
// Returns:
//   - []string: CLI arguments for even-partition fields.
//   - map[string]string: Environment for odd-partition fields.
func (c *WatchtowerConfig) renderMixed() ([]string, map[string]string) {
	args := make([]string, 0)
	env := make(map[string]string)

	for _, spec := range specs() {
		set, value := spec.get(*c)
		if !set {
			continue
		}

		useEnv := spec.env != "" && mixedToEnv(spec.flag)
		if useEnv {
			env[spec.env] = value

			continue
		}

		switch spec.kind {
		case renderBool:
			if value == "true" {
				args = append(args, "--"+spec.flag)

				continue
			}

			args = append(args, "--"+spec.flag+"=false")
		case renderSlice, renderScalar:
			args = append(args, "--"+spec.flag, value)
		}
	}

	return args, env
}

// mixedToEnv is true when a flag name's first byte is odd (stable 50/50 split).
//
// Parameters:
//   - flag: Long flag name.
//
// Returns:
//   - bool: True when the field should use environment in mixed channel.
func mixedToEnv(flag string) bool {
	if flag == "" {
		return false
	}

	return flag[0]%2 == 1
}

// FormatArgv joins argv for artifact meta.
//
// Parameters:
//   - args: CLI arguments.
//
// Returns:
//   - string: Space-joined argv.
func FormatArgv(args []string) string {
	return strings.Join(args, " ")
}

// FormatEnv formats an environment map as KEY=value lines.
//
// Parameters:
//   - env: Environment map.
//
// Returns:
//   - string: Stable sorted KEY=value block.
func FormatEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, env[key]))
	}

	return strings.Join(lines, "\n")
}
