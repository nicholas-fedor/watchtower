package settings

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/api"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/client"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/compatibility"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/filter"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/lifecycle"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/mode"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/schedule"
	"github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	dockerFlags "github.com/nicholas-fedor/watchtower/internal/settings/flags/docker"
	"github.com/nicholas-fedor/watchtower/internal/settings/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/settings/units"
)

// flagValues reads settings from the root command's prepared flags.
//
// Load applies the environment to unset flags first, so each flag holds the
// command-line value, the environment value or its default. A read of a flag
// that is not registered with the expected type returns the zero value and
// records the first such error, which Load returns.
type flagValues struct {
	flags *pflag.FlagSet // Prepared persistent flags of the root command.
	err   error          // First failed read, or nil.
}

// diskSpacePercentBase is the divisor used to convert a 0-100 percentage into a fraction of max.
const diskSpacePercentBase = 100

var (
	// errRollingRestartWithMonitorOnly indicates incompatible rolling-restart and monitor-only flags.
	errRollingRestartWithMonitorOnly = errors.New(
		"rolling-restart and monitor-only cannot both be enabled",
	)
	// errDiskSpaceMaxPercent indicates disk-space-max was given as a percentage.
	errDiskSpaceMaxPercent = errors.New("disk-space-max cannot be a percentage")
	// errDiskSpacePercentWithoutMax indicates a percentage warn value without disk-space-max.
	errDiskSpacePercentWithoutMax = errors.New(
		"disk-space-warn percentage requires disk-space-max",
	)
	// errDiskSpacePercentOutOfRange indicates a percentage outside (0, 100).
	errDiskSpacePercentOutOfRange = errors.New(
		"disk-space percentage must be greater than 0 and less than 100",
	)
	// errDiskSpaceWarnNotBelowMax indicates the warn threshold is not below the max.
	errDiskSpaceWarnNotBelowMax = errors.New(
		"disk-space-warn must be below disk-space-max",
	)
	// errTLSVerifyWithoutCertPath indicates TLS verification for a remote host
	// without the certificates that make the connection use TLS.
	errTLSVerifyWithoutCertPath = errors.New(
		"tlsverify requires cert-path for a remote Docker host",
	)
)

// Load prepares the flags of a parsed root command and reads the resolved
// settings into Config.
//
// It first prepares the flags, which sets up logging and reads secret files
// into their flags. Load changes the flags this way, so call it once per parsed
// command. It does not re-parse CLI arguments.
//
// Settings are then read from the prepared flags, which hold the command-line
// value, the environment value or the default of each setting.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. Load logs its own
//     diagnostics to a replacement that writes to standard error in the
//     configured format and level.
//   - cmd: Root command with its persistent flags registered and parsed.
//   - args: Positional container name arguments for filtering.
//
// Returns:
//   - Config: Immutable configuration snapshot.
//   - *zerolog.Logger: The logger with the configured format and level, also on
//     a failure, for reporting it.
//   - error: Non-nil when a flag cannot be prepared or read or holds an
//     invalid value. A failed preparation names its step.
func Load(log *zerolog.Logger, cmd *cobra.Command, args []string) (Config, *zerolog.Logger, error) {
	flagSet := cmd.PersistentFlags()

	log, err := prepareFlags(log, flagSet)
	if err != nil {
		return Config{}, log, err
	}

	values := &flagValues{flags: flagSet}

	err = checkRanges(values, AllSpecs())
	if err != nil {
		return Config{}, log, err
	}

	cfg := Config{}

	cfg.Docker, err = loadDocker(log, values)
	if err != nil {
		return Config{}, log, err
	}

	cfg.Client = loadClient(values)
	cfg.Compatibility = loadCompat(values)

	cfg.Schedule = loadSchedule(values)
	cfg.Mode = loadMode(values)

	cfg.Update, err = loadUpdate(log, values)
	if err != nil {
		return Config{}, log, err
	}

	cfg.Lifecycle = loadLifecycle(values)

	cfg.Filter, err = loadFilter(log, values, args)
	if err != nil {
		return Config{}, log, err
	}

	cfg.Registry = loadRegistry(values)

	cfg.API = loadAPI(values)
	cfg.Notify = loadNotify(values)
	cfg.Logging = loadLogging(values)

	cfg.Git, err = loadGit(values)
	if err != nil {
		return Config{}, log, err
	}

	if values.err != nil {
		return Config{}, log, values.err
	}

	err = validate(log, cfg)
	if err != nil {
		return Config{}, log, err
	}

	return cfg, log, nil
}

// LoadDocker reads only the Docker connection settings of a parsed root
// command.
//
// It serves a process whose [Load] did not finish or never ran, so the
// environment may not be applied to the flags yet. LoadDocker applies the
// environment to the Docker settings' flags itself and reads them as Load does.
//
// Parameters:
//   - log: Logger for the TLS warnings.
//   - cmd: Root command with its persistent flags registered and parsed.
//
// Returns:
//   - docker.Docker: The Docker connection settings.
//   - error: Non-nil when an environment value or a flag cannot be read, or
//     when the settings ask for TLS verification of a remote host without a
//     certificate path.
func LoadDocker(log *zerolog.Logger, cmd *cobra.Command) (docker.Docker, error) {
	flagSet := cmd.PersistentFlags()

	err := applyEnvToFlags(flagSet, dockerFlags.Specs())
	if err != nil {
		return docker.Docker{}, fmt.Errorf("apply environment configuration: %w", err)
	}

	values := &flagValues{flags: flagSet}

	settings, err := loadDocker(log, values)
	if err != nil {
		return docker.Docker{}, err
	}

	if values.err != nil {
		return docker.Docker{}, values.err
	}

	return settings, nil
}

// checkRanges returns an error for the first integer or duration setting that
// cannot be read or whose value is outside the range its spec allows.
//
// Parameters:
//   - values: The prepared flags.
//   - specs: Specs of every setting.
//
// Returns:
//   - error: The failed read of a setting, or the spec's range error naming
//     the setting and its value.
func checkRanges(values *flagValues, specs []spec.FlagSpec) error {
	for _, flagSpec := range specs {
		var err error

		switch flagSpec.Kind {
		case spec.KindInt:
			err = flagSpec.CheckInt(values.Int(flagSpec.Name))
		case spec.KindDuration:
			err = flagSpec.CheckDuration(values.Duration(flagSpec.Name))
		case spec.KindBool, spec.KindString, spec.KindStringSlice, spec.KindStringArray:
		default:
		}

		// A setting that cannot be read holds 0, which the range check would
		// report as an out-of-range value instead of the failed read.
		if values.err != nil {
			return values.err
		}

		if err != nil {
			return err //nolint:wrapcheck // The spec's error already names the setting and its value.
		}
	}

	return nil
}

// loadDocker reads Docker connection settings.
//
// An https:// host becomes tcp://. With TLS verification on, a remote host
// needs a certificate path, and a host that cannot use TLS logs a warning.
//
// Parameters:
//   - log: Logger for the TLS warnings.
//   - values: The prepared flags.
//
// Returns:
//   - docker.Docker: The Docker connection settings.
//   - error: Non-nil when TLS verification is on for a remote host without a
//     certificate path, since the client would then connect without TLS.
func loadDocker(log *zerolog.Logger, values *flagValues) (docker.Docker, error) {
	settings := docker.Docker{
		Host:       values.String("host"),
		TLSVerify:  values.Bool("tlsverify"),
		APIVersion: strings.Trim(values.String("api-version"), "\""),
		CertPath:   values.String("cert-path"),
	}

	// The certificate path, not the scheme, makes the connection use TLS.
	// Neither Docker's CLI nor the Docker SDK's attach and exec connections,
	// which dial with the scheme as the network name, can use an https:// host.
	if address, ok := strings.CutPrefix(settings.Host, "https://"); ok {
		settings.Host = "tcp://" + address
	}

	if !settings.TLSVerify {
		return settings, nil
	}

	// The default host is the local socket, where TLS does not apply.
	socket := settings.Host == "" ||
		strings.HasPrefix(settings.Host, "unix://") ||
		strings.HasPrefix(settings.Host, "npipe://")
	if settings.CertPath == "" && !socket {
		return docker.Docker{}, errTLSVerifyWithoutCertPath
	}

	switch {
	case strings.HasPrefix(settings.Host, "http://"):
		log.Warn().
			Msg("TLS verification is enabled but DOCKER_HOST uses insecure scheme 'http://'. Consider using 'tcp://' or disable TLS verification.")
	case strings.HasPrefix(settings.Host, "unix://"):
		log.Warn().
			Msg("TLS verification is enabled but DOCKER_HOST uses local socket 'unix://'. TLS is not applicable for local sockets. Consider disabling TLS verification.")
	default:
	}

	return settings, nil
}

// loadClient reads Docker client construction settings.
func loadClient(values *flagValues) client.Client {
	return client.Client{
		IncludeStopped:    values.Bool("include-stopped"),
		IncludeRestarting: values.Bool("include-restarting"),
		ReviveStopped:     values.Bool("revive-stopped"),
		RemoveVolumes:     values.Bool("remove-volumes"),
		WarnOnHeadFailure: values.String("warn-on-head-failure"),
	}
}

// loadCompat reads runtime compatibility settings.
func loadCompat(values *flagValues) compatibility.Compatibility {
	return compatibility.Compatibility{
		DisableMemorySwappiness: values.Bool("disable-memory-swappiness"),
		CPUCopyMode:             values.String("cpu-copy-mode"),
	}
}

// loadSchedule reads schedule settings.
func loadSchedule(values *flagValues) schedule.Schedule {
	return schedule.Schedule{
		IntervalSeconds: values.Int("interval"),
		Spec:            values.String("schedule"),
		UpdateOnStart:   values.Bool("update-on-start"),
	}
}

// loadMode reads process mode settings.
func loadMode(values *flagValues) mode.Mode {
	return mode.Mode{
		RunOnce:                values.Bool("run-once"),
		HealthCheck:            values.Bool("health-check"),
		Porcelain:              values.String("porcelain"),
		SelfUpdateOrchestrator: values.Bool("self-update-orchestrator"),
		NoStartupMessage:       values.Bool("no-startup-message"),
	}
}

// loadUpdate reads update policy settings.
func loadUpdate(log *zerolog.Logger, values *flagValues) (update.Update, error) {
	stopTimeout := values.Duration("stop-timeout")

	if stopTimeout > 0 && stopTimeout < time.Second {
		log.Warn().
			Dur("timeout", stopTimeout).
			Msg("WATCHTOWER_TIMEOUT is less than 1 second")
	}

	cooldownStr := values.String("cooldown-delay")

	var cooldown time.Duration

	if cooldownStr != "" {
		parsed, err := units.ParseDuration(cooldownStr)
		if err != nil {
			return update.Update{}, fmt.Errorf("cooldown-delay: %w", err)
		}

		cooldown = parsed
	}

	maxRaw := values.String("disk-space-max")
	warnRaw := values.String("disk-space-warn")

	maxBytes, warnBytes, err := resolveDiskSpaceThresholds(maxRaw, warnRaw)
	if err != nil {
		return update.Update{}, err
	}

	return update.Update{
		Cleanup:             values.Bool("cleanup"),
		NoPull:              values.Bool("no-pull"),
		EnableChangelog:     values.Bool("enable-changelog"),
		NoRestart:           values.Bool("no-restart"),
		MonitorOnly:         values.Bool("monitor-only"),
		RollingRestart:      values.Bool("rolling-restart"),
		StopTimeout:         stopTimeout,
		CooldownDelay:       cooldown,
		UseComposeDependsOn: values.Bool("use-compose-depends-on"),
		LabelPrecedence:     values.Bool("label-take-precedence"),
		EphemeralSelfUpdate: values.Bool("ephemeral-self-update"),
		DiskSpaceMax:        maxRaw,
		DiskSpaceWarn:       warnRaw,
		DiskSpaceMaxBytes:   maxBytes,
		DiskSpaceWarnBytes:  warnBytes,
	}, nil
}

// resolveDiskSpaceThresholds parses disk-space-max and disk-space-warn into bytes.
//
// Max must be an absolute size. Warn may be absolute or a percent of max.
// When both are set, warn must be strictly below max.
//
// Parameters:
//   - maxRaw: Raw disk-space-max value.
//   - warnRaw: Raw disk-space-warn value.
//
// Returns:
//   - int64: Parsed max in bytes, or 0 when unset.
//   - int64: Parsed warn in bytes, or 0 when unset.
//   - error: Non-nil when values are invalid or inconsistent.
func resolveDiskSpaceThresholds(maxRaw, warnRaw string) (int64, int64, error) {
	maxRaw = strings.TrimSpace(maxRaw)
	warnRaw = strings.TrimSpace(warnRaw)

	if strings.HasSuffix(maxRaw, "%") {
		return 0, 0, errDiskSpaceMaxPercent
	}

	maxBytes, err := units.ParseDiskSpace(maxRaw)
	if err != nil {
		return 0, 0, fmt.Errorf("disk-space-max: %w", err)
	}

	warnBytes, err := parseDiskSpaceWarn(warnRaw, maxBytes)
	if err != nil {
		return 0, 0, err
	}

	if maxBytes > 0 && warnBytes > 0 && warnBytes >= maxBytes {
		return 0, 0, errDiskSpaceWarnNotBelowMax
	}

	return maxBytes, warnBytes, nil
}

// parseDiskSpaceWarn parses an absolute or percentage warning threshold.
//
// Parameters:
//   - raw: Raw disk-space-warn value.
//   - maxBytes: Parsed disk-space-max in bytes; required when raw is a percentage.
//
// Returns:
//   - int64: Parsed warn threshold in bytes, or 0 when unset.
//   - error: Non-nil when the value is invalid.
func parseDiskSpaceWarn(raw string, maxBytes int64) (int64, error) {
	if raw == "" || raw == "0" {
		return 0, nil
	}

	if strings.HasSuffix(raw, "%") {
		if maxBytes <= 0 {
			return 0, errDiskSpacePercentWithoutMax
		}

		pct, err := units.ParseDiskSpacePercent(strings.TrimSuffix(raw, "%"))
		if err != nil {
			return 0, fmt.Errorf("disk-space-warn: %w", err)
		}

		if pct <= 0 || pct >= diskSpacePercentBase {
			return 0, errDiskSpacePercentOutOfRange
		}

		return int64(float64(maxBytes) * pct / diskSpacePercentBase), nil
	}

	warnBytes, err := units.ParseDiskSpace(raw)
	if err != nil {
		return 0, fmt.Errorf("disk-space-warn: %w", err)
	}

	return warnBytes, nil
}

// loadLifecycle reads lifecycle hook settings.
func loadLifecycle(values *flagValues) lifecycle.Lifecycle {
	return lifecycle.Lifecycle{
		Enabled: values.Bool("enable-lifecycle-hooks"),
		UID:     values.Int("lifecycle-uid"),
		GID:     values.Int("lifecycle-gid"),
	}
}

// Bool returns a boolean setting.
//
// Parameters:
//   - name: Flag name.
//
// Returns:
//   - bool: The flag's value, or false after a failed read.
func (v *flagValues) Bool(name string) bool {
	value, err := v.flags.GetBool(name)
	v.record(name, err)

	return value
}

// String returns a string setting.
//
// Parameters:
//   - name: Flag name.
//
// Returns:
//   - string: The flag's value, or "" after a failed read.
func (v *flagValues) String(name string) string {
	value, err := v.flags.GetString(name)
	v.record(name, err)

	return value
}

// Int returns an integer setting.
//
// Parameters:
//   - name: Flag name.
//
// Returns:
//   - int: The flag's value, or 0 after a failed read.
func (v *flagValues) Int(name string) int {
	value, err := v.flags.GetInt(name)
	v.record(name, err)

	return value
}

// Duration returns a duration setting.
//
// Parameters:
//   - name: Flag name.
//
// Returns:
//   - time.Duration: The flag's value, or 0 after a failed read.
func (v *flagValues) Duration(name string) time.Duration {
	value, err := v.flags.GetDuration(name)
	v.record(name, err)

	return value
}

// Strings returns a copy of a list setting.
//
// Parameters:
//   - name: Flag name.
//
// Returns:
//   - []string: The values, never nil. Empty after a failed read.
func (v *flagValues) Strings(name string) []string {
	flag := v.flags.Lookup(name)
	if flag == nil {
		v.record(name, errFlagNotRegistered)

		return []string{}
	}

	sliceValue, ok := flag.Value.(pflag.SliceValue)
	if !ok {
		v.record(name, errUnsupportedFlagKind)

		return []string{}
	}

	// GetSlice of a string slice flag returns the flag's own slice, so callers
	// that change elements need this copy.
	return append([]string{}, sliceValue.GetSlice()...)
}

// record keeps the first failed read.
//
// Parameters:
//   - name: Flag name that was read.
//   - err: The read's error, or nil.
func (v *flagValues) record(name string, err error) {
	if err != nil && v.err == nil {
		v.err = fmt.Errorf("read %s: %w", name, err)
	}
}

// normalizedStringSlice reads a list flag and applies normalize to each element.
//
// Parameters:
//   - values: The prepared flags.
//   - name: Flag name.
//   - normalize: Per-element transform (for example TrimSpace or NormalizeContainerName).
//
// Returns:
//   - []string: Normalized list (empty when unset).
func normalizedStringSlice(values *flagValues, name string, normalize func(string) string) []string {
	list := values.Strings(name)
	for i := range list {
		list[i] = normalize(list[i])
	}

	return list
}

// loadFilter reads filter settings, normalizes names, and builds the predicate.
func loadFilter(log *zerolog.Logger, values *flagValues, args []string) (filter.Filter, error) {
	labelEnable := values.Bool("label-enable")

	disableContainers := normalizedStringSlice(values, "disable-containers", container.NormalizeContainerName)

	monitorImages := normalizedStringSlice(values, "monitor-image-names", strings.TrimSpace)

	skipImages := normalizedStringSlice(values, "skip-image-names", strings.TrimSpace)

	enableByLabel := values.Strings("enable-containers-by-label")

	disableByLabel := values.Strings("disable-containers-by-label")

	scope := values.String("scope")

	names := make([]string, len(args))
	for i, name := range args {
		names[i] = container.NormalizeContainerName(name)
	}

	inputs := filter.Filter{
		LabelEnable:              labelEnable,
		DisableContainers:        disableContainers,
		MonitorImageNames:        monitorImages,
		SkipImageNames:           skipImages,
		EnableContainersByLabel:  enableByLabel,
		DisableContainersByLabel: disableByLabel,
		Scope:                    scope,
		Names:                    names,
	}

	return inputs.Build(log) //nolint:wrapcheck // Build names the failure.
}

// loadRegistry reads the registry request settings.
//
// The TLS settings come from the flags. The cooldown platform has no flags and
// comes from the environment only.
//
// Parameters:
//   - values: The prepared flags.
//
// Returns:
//   - registry.Registry: Registry request settings.
func loadRegistry(values *flagValues) registry.Registry {
	return registry.Registry{
		CooldownPlatform: registry.Platform{
			OS:           os.Getenv("WATCHTOWER_COOLDOWN_PLATFORM_OS"),
			Architecture: os.Getenv("WATCHTOWER_COOLDOWN_PLATFORM_ARCH"),
			Variant:      os.Getenv("WATCHTOWER_COOLDOWN_PLATFORM_VARIANT"),
		},
		TLSMinVersion: values.String("registry-tls-min-version"),
		TLSSkip:       values.Bool("registry-tls-skip"),
	}
}

// loadAPI reads HTTP API settings.
func loadAPI(values *flagValues) api.API {
	checkTimeout := values.Duration("http-api-check-timeout")
	updateTimeout := values.Duration("http-api-update-timeout")

	return api.API{
		Endpoints:            values.Strings("http-api-endpoints"),
		LegacyUpdate:         values.Bool("http-api-update"),
		LegacyMetrics:        values.Bool("http-api-metrics"),
		LegacyContainers:     values.Bool("http-api-containers"),
		Host:                 values.String("http-api-host"),
		HostChanged:          values.flags.Changed("http-api-host"),
		Port:                 values.String("http-api-port"),
		PortChanged:          values.flags.Changed("http-api-port"),
		Token:                values.String("http-api-token"),
		EventsToken:          values.String("http-api-events-token"),
		PeriodicPolls:        values.Bool("http-api-periodic-polls"),
		RateLimit:            values.Int("http-api-rate-limit"),
		RateLimitChanged:     values.flags.Changed("http-api-rate-limit"),
		TLSCert:              values.String("http-api-tls-cert"),
		TLSKey:               values.String("http-api-tls-key"),
		TrustedProxies:       values.Strings("http-api-trusted-proxies"),
		ProxyHeader:          values.String("http-api-proxy-header"),
		CORSOrigins:          values.Strings("http-api-cors-origins"),
		CheckTimeout:         checkTimeout,
		CheckTimeoutChanged:  values.flags.Changed("http-api-check-timeout"),
		UpdateTimeout:        updateTimeout,
		UpdateTimeoutChanged: values.flags.Changed("http-api-update-timeout"),
	}
}

// loadNotify reads notification settings.
func loadNotify(values *flagValues) notify.Notify {
	return notify.Notify{
		URLs:             values.Strings("notification-url"),
		LegacyTypes:      values.Strings("notifications"),
		Level:            values.String("notifications-level"),
		Template:         values.String("notification-template"),
		TemplateFile:     values.String("notification-template-file"),
		Report:           values.Bool("notification-report"),
		SplitByContainer: values.Bool("notification-split-by-container"),
		SkipTitle:        values.Bool("notification-skip-title"),
		LogStdout:        values.Bool("notification-log-stdout"),
		DelaySeconds:     values.Int("notifications-delay"),
		Hostname:         values.String("notifications-hostname"),
		TitleTag:         values.String("notification-title-tag"),
		EmailSubjectTag:  values.String("notification-email-subjecttag"),
		Legacy: notify.Legacy{
			EmailFrom:           values.String("notification-email-from"),
			EmailTo:             values.String("notification-email-to"),
			EmailServer:         values.String("notification-email-server"),
			EmailUser:           values.String("notification-email-server-user"),
			EmailPassword:       values.String("notification-email-server-password"),
			EmailPort:           values.Int("notification-email-server-port"),
			EmailTLSSkipVerify:  values.Bool("notification-email-server-tls-skip-verify"),
			EmailDelay:          values.Int("notification-email-delay"),
			SlackHookURL:        values.String("notification-slack-hook-url"),
			SlackIdentifier:     values.String("notification-slack-identifier"),
			SlackChannel:        values.String("notification-slack-channel"),
			SlackIconEmoji:      values.String("notification-slack-icon-emoji"),
			SlackIconURL:        values.String("notification-slack-icon-url"),
			MSTeamsHook:         values.String("notification-msteams-hook"),
			GotifyURL:           values.String("notification-gotify-url"),
			GotifyToken:         values.String("notification-gotify-token"),
			GotifyTLSSkipVerify: values.Bool("notification-gotify-tls-skip-verify"),
		},
	}
}

// loadLogging reads logging settings.
//
// Parameters:
//   - values: The prepared flags. The no-color flag holds NO_COLOR's
//     presence-based value when the command line left it unset.
//
// Returns:
//   - logging.Logging: Logging settings.
func loadLogging(values *flagValues) logging.Logging {
	format := values.String("log-format")
	if format == "" {
		format = "auto"
	}

	return logging.Logging{
		Level:   values.String("log-level"),
		Format:  format,
		Debug:   values.Bool("debug"),
		Trace:   values.Bool("trace"),
		NoColor: values.Bool("no-color"),
	}
}

// loadGit reads Git association and watcher settings.
//
// Parameters:
//   - values: The prepared flags.
//
// Returns:
//   - cfggit.Git: Parsed Git settings.
//   - error: Non-nil when a mapping, host, or CA bundle is invalid.
func loadGit(values *flagValues) (gitConfig.Git, error) {
	rawImages := values.Strings("git-image")

	images, err := gitConfig.ParseImageMappings(rawImages)
	if err != nil {
		return gitConfig.Git{}, fmt.Errorf("git-image: %w", err)
	}

	caBundle, err := readOptionalFile(values.String("git-ca-bundle"))
	if err != nil {
		return gitConfig.Git{}, fmt.Errorf("git-ca-bundle: %w", err)
	}

	rawProjects := values.Strings("compose-project")

	projects, err := gitConfig.ParseComposeProjects(rawProjects)
	if err != nil {
		return gitConfig.Git{}, fmt.Errorf("compose-project: %w", err)
	}

	timeout := values.Duration("git-timeout")

	return gitConfig.Git{
		Enable:          values.Bool("git-enable"),
		DefaultRef:      gitConfig.DefaultRef,
		SemverPolicy:    gitConfig.DefaultPolicy,
		Images:          images,
		Dockerfile:      strings.TrimSpace(values.String("git-dockerfile")),
		Context:         strings.TrimSpace(values.String("git-context")),
		Timeout:         timeout,
		Token:           values.String("git-auth-token"),
		Username:        values.String("git-username"),
		Password:        values.String("git-password"),
		SSHKeyPath:      values.String("git-ssh-key-path"),
		SSHKnownHosts:   values.String("git-ssh-known-hosts"),
		CABundle:        caBundle,
		InsecureSkipTLS: values.Bool("git-insecure-skip-tls"),
		ComposeStash:    values.Bool("git-compose-stash"),
		ComposeProjects: projects,
	}, nil
}

// readOptionalFile reads path when it is non-empty.
//
// Parameters:
//   - path: Filesystem path, or empty.
//
// Returns:
//   - []byte: File contents, or nil when path is empty.
//   - error: Non-nil when the file cannot be read.
func readOptionalFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return data, nil
}

// validate checks cross-flag constraints that Load can enforce without side effects.
func validate(log *zerolog.Logger, cfg Config) error {
	if cfg.Update.RollingRestart && cfg.Update.MonitorOnly {
		return errRollingRestartWithMonitorOnly
	}

	if cfg.Update.MonitorOnly && cfg.Update.NoPull {
		log.Warn().
			Bool("monitor_only", cfg.Update.MonitorOnly).
			Bool("no_pull", cfg.Update.NoPull).
			Msg("Combining monitor-only and no-pull might result in no updates")
	}

	return nil
}
