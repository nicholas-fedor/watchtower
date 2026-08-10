package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/config/api"
	"github.com/nicholas-fedor/watchtower/internal/config/client"
	"github.com/nicholas-fedor/watchtower/internal/config/compatibility"
	"github.com/nicholas-fedor/watchtower/internal/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/config/filter"
	"github.com/nicholas-fedor/watchtower/internal/config/lifecycle"
	"github.com/nicholas-fedor/watchtower/internal/config/logging"
	"github.com/nicholas-fedor/watchtower/internal/config/mode"
	"github.com/nicholas-fedor/watchtower/internal/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/config/registry"
	"github.com/nicholas-fedor/watchtower/internal/config/schedule"
	"github.com/nicholas-fedor/watchtower/internal/config/update"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
)

var (
	// ErrNegativeStopTimeout indicates stop-timeout was set to a negative duration.
	ErrNegativeStopTimeout = errors.New("stop-timeout must be non-negative")
	// ErrNegativeCooldownDelay indicates cooldown-delay was set to a negative duration.
	ErrNegativeCooldownDelay = errors.New("cooldown-delay must be non-negative")
	// ErrRollingRestartWithMonitorOnly indicates incompatible rolling-restart and monitor-only flags.
	ErrRollingRestartWithMonitorOnly = errors.New(
		"rolling-restart and monitor-only cannot both be enabled",
	)
)

// Load reads resolved settings from a parsed Cobra command into Config.
//
// Callers must run flag registration, ProcessFlagAliases, SetupLogging, and
// GetSecretsFromFiles before Load. Load does not re-parse CLI arguments.
//
// Values are resolved through a process-local Viper instance (flag > env >
// static default) using FlagSpec metadata from every domain.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - cmd: Parsed root command with persistent flags populated.
//   - args: Positional container name arguments for filtering.
//
// Returns:
//   - Config: Immutable configuration snapshot.
//   - error: Non-nil when required flags are missing or values are invalid.
func Load(log *zerolog.Logger, cmd *cobra.Command, args []string) (Config, error) {
	flagSet := cmd.PersistentFlags()

	vip := viper.New()

	err := flags.BindAll(vip, flagSet, flags.AllSpecs())
	if err != nil {
		return Config{}, fmt.Errorf("bind configuration: %w", err)
	}

	cfg := Config{}

	cfg.Docker = loadDocker(vip)
	cfg.Client = loadClient(vip)
	cfg.Compatibility = loadCompat(vip)

	// Keep client and compat CPU/memory fields aligned for projections.
	if cfg.Client.CPUCopyMode == "" {
		cfg.Client.CPUCopyMode = cfg.Compatibility.CPUCopyMode
	}

	if !cfg.Client.DisableMemorySwappiness {
		cfg.Client.DisableMemorySwappiness = cfg.Compatibility.DisableMemorySwappiness
	}

	cfg.Schedule = loadSchedule(vip)
	cfg.Mode = loadMode(vip)

	cfg.Update, err = loadUpdate(log, vip, flagSet)
	if err != nil {
		return Config{}, err
	}

	cfg.Lifecycle = loadLifecycle(vip)

	cfg.Filter, err = loadFilter(log, vip, flagSet, args)
	if err != nil {
		return Config{}, err
	}

	cfg.Registry = loadRegistry(vip)
	cfg.API = loadAPI(vip, flagSet)
	cfg.Notify = loadNotify(vip, flagSet)
	cfg.Logging = loadLogging(vip)

	err = validate(log, cfg)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// loadDocker reads Docker connection settings from Viper after BindAll.
func loadDocker(v *viper.Viper) docker.Docker {
	return docker.Docker{
		Host:       v.GetString("host"),
		TLSVerify:  v.GetBool("tlsverify"),
		APIVersion: strings.Trim(v.GetString("api-version"), "\""),
		CertPath:   v.GetString("cert-path"),
	}
}

// loadClient reads Docker client construction settings from Viper.
func loadClient(vip *viper.Viper) client.Client {
	return client.Client{
		IncludeStopped:    vip.GetBool("include-stopped"),
		IncludeRestarting: vip.GetBool("include-restarting"),
		ReviveStopped:     vip.GetBool("revive-stopped"),
		RemoveVolumes:     vip.GetBool("remove-volumes"),
		WarnOnHeadFailure: vip.GetString("warn-on-head-failure"),
	}
}

// loadCompat reads runtime compatibility settings from Viper.
func loadCompat(vip *viper.Viper) compatibility.Compatibility {
	return compatibility.Compatibility{
		DisableMemorySwappiness: vip.GetBool("disable-memory-swappiness"),
		CPUCopyMode:             vip.GetString("cpu-copy-mode"),
	}
}

// loadSchedule reads schedule settings from Viper.
func loadSchedule(vip *viper.Viper) schedule.Schedule {
	return schedule.Schedule{
		IntervalSeconds: vip.GetInt("interval"),
		Spec:            vip.GetString("schedule"),
		UpdateOnStart:   vip.GetBool("update-on-start"),
	}
}

// loadMode reads process mode settings from Viper.
func loadMode(vip *viper.Viper) mode.Mode {
	return mode.Mode{
		RunOnce:                vip.GetBool("run-once"),
		HealthCheck:            vip.GetBool("health-check"),
		Porcelain:              vip.GetString("porcelain"),
		SelfUpdateOrchestrator: vip.GetBool("self-update-orchestrator"),
		NoStartupMessage:       vip.GetBool("no-startup-message"),
	}
}

// loadUpdate reads update policy settings from Viper.
func loadUpdate(log *zerolog.Logger, vip *viper.Viper, flagSet *pflag.FlagSet) (update.Update, error) {
	stopTimeout := durationValue(vip, flagSet, "stop-timeout", []string{"WATCHTOWER_TIMEOUT"})

	if stopTimeout < 0 {
		return update.Update{}, ErrNegativeStopTimeout
	}

	if stopTimeout > 0 && stopTimeout < time.Second {
		log.Warn().
			Dur("timeout", stopTimeout).
			Msg("WATCHTOWER_TIMEOUT is less than 1 second")
	}

	cooldownStr := vip.GetString("cooldown-delay")

	var cooldown time.Duration

	if cooldownStr != "" {
		parsed, err := util.ParseDuration(cooldownStr)
		if err != nil {
			return update.Update{}, fmt.Errorf("cooldown-delay: %w", err)
		}

		if parsed < 0 {
			return update.Update{}, ErrNegativeCooldownDelay
		}

		cooldown = parsed
	}

	return update.Update{
		Cleanup:             vip.GetBool("cleanup"),
		NoPull:              vip.GetBool("no-pull"),
		NoRestart:           vip.GetBool("no-restart"),
		MonitorOnly:         vip.GetBool("monitor-only"),
		RollingRestart:      vip.GetBool("rolling-restart"),
		StopTimeout:         stopTimeout,
		CooldownDelay:       cooldown,
		UseComposeDependsOn: vip.GetBool("use-compose-depends-on"),
		LabelPrecedence:     vip.GetBool("label-take-precedence"),
		EphemeralSelfUpdate: vip.GetBool("ephemeral-self-update"),
	}, nil
}

// loadLifecycle reads lifecycle hook settings from Viper.
func loadLifecycle(vip *viper.Viper) lifecycle.Lifecycle {
	return lifecycle.Lifecycle{
		Enabled: vip.GetBool("enable-lifecycle-hooks"),
		UID:     vip.GetInt("lifecycle-uid"),
		GID:     vip.GetInt("lifecycle-gid"),
	}
}

// normalizedStringSlice loads a list flag/env value and applies normalize to each element.
//
// Parameters:
//   - vip: Bound Viper instance.
//   - flagSet: Parsed flag set.
//   - name: Flag name.
//   - envKeys: Environment variable aliases.
//   - parse: List parse strategy.
//   - normalize: Per-element transform (for example TrimSpace or NormalizeContainerName).
//
// Returns:
//   - []string: Normalized list (empty when unset).
func normalizedStringSlice(
	vip *viper.Viper,
	flagSet *pflag.FlagSet,
	name string,
	envKeys []string,
	parse spec.ListParseKind,
	normalize func(string) string,
) []string {
	values := stringSliceValue(vip, flagSet, name, envKeys, parse)
	for i := range values {
		values[i] = normalize(values[i])
	}

	return values
}

// loadFilter reads filter settings, normalizes names, and builds the predicate.
func loadFilter(log *zerolog.Logger, vip *viper.Viper, flagSet *pflag.FlagSet, args []string) (filter.Filter, error) {
	labelEnable := vip.GetBool("label-enable")

	disableContainers := normalizedStringSlice(
		vip, flagSet, "disable-containers",
		[]string{"WATCHTOWER_DISABLE_CONTAINERS"},
		spec.ListCommaOrSpace,
		util.NormalizeContainerName,
	)

	monitorImages := normalizedStringSlice(
		vip, flagSet, "monitor-image-names",
		[]string{"WATCHTOWER_MONITOR_IMAGE_NAMES"},
		spec.ListCommaOrSpace,
		strings.TrimSpace,
	)

	skipImages := normalizedStringSlice(
		vip, flagSet, "skip-image-names",
		[]string{"WATCHTOWER_SKIP_IMAGE_NAMES"},
		spec.ListCommaOrSpace,
		strings.TrimSpace,
	)

	enableByLabel := stringSliceValue(
		vip, flagSet, "enable-containers-by-label",
		[]string{"WATCHTOWER_ENABLE_CONTAINERS_BY_LABEL"},
		spec.ListCommaOnly,
	)

	disableByLabel := stringSliceValue(
		vip, flagSet, "disable-containers-by-label",
		[]string{"WATCHTOWER_DISABLE_CONTAINERS_BY_LABEL"},
		spec.ListCommaOnly,
	)

	scope := vip.GetString("scope")

	names := make([]string, len(args))
	for i, name := range args {
		names[i] = util.NormalizeContainerName(name)
	}

	predicate, desc, err := filters.BuildFilter(
		log,
		names,
		disableContainers,
		monitorImages,
		skipImages,
		enableByLabel,
		disableByLabel,
		labelEnable,
		scope,
	)
	if err != nil {
		return filter.Filter{}, fmt.Errorf("build filter: %w", err)
	}

	return filter.Filter{
		LabelEnable:              labelEnable,
		DisableContainers:        disableContainers,
		MonitorImageNames:        monitorImages,
		SkipImageNames:           skipImages,
		EnableContainersByLabel:  enableByLabel,
		DisableContainersByLabel: disableByLabel,
		Scope:                    scope,
		Names:                    names,
		Predicate:                predicate,
		Desc:                     desc,
	}, nil
}

// loadRegistry reads registry TLS settings from Viper.
func loadRegistry(vip *viper.Viper) registry.Registry {
	return registry.Registry{
		TLSSkip:       vip.GetBool("registry-tls-skip"),
		TLSMinVersion: vip.GetString("registry-tls-min-version"),
	}
}

// loadAPI reads HTTP API settings from Viper.
func loadAPI(vip *viper.Viper, flagSet *pflag.FlagSet) api.API {
	return api.API{
		Endpoints: stringSliceValue(
			vip, flagSet, "http-api-endpoints",
			[]string{"WATCHTOWER_HTTP_API_ENDPOINTS"},
			spec.ListCommaOrSpace,
		),
		LegacyUpdate:     vip.GetBool("http-api-update"),
		LegacyMetrics:    vip.GetBool("http-api-metrics"),
		LegacyContainers: vip.GetBool("http-api-containers"),
		Host:             vip.GetString("http-api-host"),
		HostChanged:      flagChanged(flagSet, "http-api-host"),
		Port:             vip.GetString("http-api-port"),
		PortChanged:      flagChanged(flagSet, "http-api-port"),
		Token:            vip.GetString("http-api-token"),
		EventsToken:      vip.GetString("http-api-events-token"),
		PeriodicPolls:    vip.GetBool("http-api-periodic-polls"),
		RateLimit:        vip.GetInt("http-api-rate-limit"),
		RateLimitChanged: flagChanged(flagSet, "http-api-rate-limit"),
		TLSCert:          vip.GetString("http-api-tls-cert"),
		TLSKey:           vip.GetString("http-api-tls-key"),
		TrustedProxies: stringSliceValue(
			vip, flagSet, "http-api-trusted-proxies",
			[]string{"WATCHTOWER_HTTP_API_TRUSTED_PROXIES"},
			spec.ListCommaOrSpace,
		),
		ProxyHeader: vip.GetString("http-api-proxy-header"),
		CORSOrigins: stringSliceValue(
			vip, flagSet, "http-api-cors-origins",
			[]string{"WATCHTOWER_HTTP_API_CORS_ORIGINS"},
			spec.ListCommaOrSpace,
		),
		CheckTimeout: durationValue(
			vip, flagSet, "http-api-check-timeout",
			[]string{"WATCHTOWER_HTTP_API_CHECK_TIMEOUT"},
		),
		CheckTimeoutChanged: flagChanged(flagSet, "http-api-check-timeout"),
		UpdateTimeout: durationValue(
			vip, flagSet, "http-api-update-timeout",
			[]string{"WATCHTOWER_HTTP_API_UPDATE_TIMEOUT"},
		),
		UpdateTimeoutChanged: flagChanged(flagSet, "http-api-update-timeout"),
	}
}

// loadNotify reads notification settings from Viper.
func loadNotify(vip *viper.Viper, flagSet *pflag.FlagSet) notify.Notify {
	return notify.Notify{
		URLs: stringSliceValue(
			vip, flagSet, "notification-url",
			[]string{"WATCHTOWER_NOTIFICATION_URL"},
			spec.ListNotificationURLs,
		),
		LegacyTypes: stringSliceValue(
			vip, flagSet, "notifications",
			[]string{"WATCHTOWER_NOTIFICATIONS"},
			spec.ListCommaOrSpace,
		),
		Level:            vip.GetString("notifications-level"),
		Template:         vip.GetString("notification-template"),
		TemplateFile:     vip.GetString("notification-template-file"),
		Report:           vip.GetBool("notification-report"),
		SplitByContainer: vip.GetBool("notification-split-by-container"),
		SkipTitle:        vip.GetBool("notification-skip-title"),
		LogStdout:        vip.GetBool("notification-log-stdout"),
		DelaySeconds:     vip.GetInt("notifications-delay"),
		Hostname:         vip.GetString("notifications-hostname"),
		TitleTag:         vip.GetString("notification-title-tag"),
		EmailSubjectTag:  vip.GetString("notification-email-subjecttag"),
		Legacy: notify.Legacy{
			EmailFrom:           vip.GetString("notification-email-from"),
			EmailTo:             vip.GetString("notification-email-to"),
			EmailServer:         vip.GetString("notification-email-server"),
			EmailUser:           vip.GetString("notification-email-server-user"),
			EmailPassword:       vip.GetString("notification-email-server-password"),
			EmailPort:           vip.GetInt("notification-email-server-port"),
			EmailTLSSkipVerify:  vip.GetBool("notification-email-server-tls-skip-verify"),
			EmailDelay:          vip.GetInt("notification-email-delay"),
			SlackHookURL:        vip.GetString("notification-slack-hook-url"),
			SlackIdentifier:     vip.GetString("notification-slack-identifier"),
			SlackChannel:        vip.GetString("notification-slack-channel"),
			SlackIconEmoji:      vip.GetString("notification-slack-icon-emoji"),
			SlackIconURL:        vip.GetString("notification-slack-icon-url"),
			MSTeamsHook:         vip.GetString("notification-msteams-hook"),
			GotifyURL:           vip.GetString("notification-gotify-url"),
			GotifyToken:         vip.GetString("notification-gotify-token"),
			GotifyTLSSkipVerify: vip.GetBool("notification-gotify-tls-skip-verify"),
		},
	}
}

// loadLogging reads logging settings from Viper.
func loadLogging(vip *viper.Viper) logging.Logging {
	format := vip.GetString("log-format")
	if format == "" {
		format = "auto"
	}

	return logging.Logging{
		Level:   vip.GetString("log-level"),
		Format:  format,
		Debug:   vip.GetBool("debug"),
		Trace:   vip.GetBool("trace"),
		NoColor: vip.GetBool("no-color"),
	}
}

// validate checks cross-flag constraints that Load can enforce without side effects.
func validate(log *zerolog.Logger, cfg Config) error {
	if cfg.Update.RollingRestart && cfg.Update.MonitorOnly {
		return ErrRollingRestartWithMonitorOnly
	}

	if cfg.Update.MonitorOnly && cfg.Update.NoPull {
		log.Warn().
			Bool("monitor_only", cfg.Update.MonitorOnly).
			Bool("no_pull", cfg.Update.NoPull).
			Msg("Combining monitor-only and no-pull might result in no updates")
	}

	return nil
}

// flagChanged reports whether a flag was set on the command line.
func flagChanged(flagSet *pflag.FlagSet, name string) bool {
	flag := flagSet.Lookup(name)
	if flag == nil {
		return false
	}

	return flag.Changed
}
