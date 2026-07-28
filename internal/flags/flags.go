package flags

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/flags/api"
	"github.com/nicholas-fedor/watchtower/internal/flags/client"
	"github.com/nicholas-fedor/watchtower/internal/flags/compat"
	"github.com/nicholas-fedor/watchtower/internal/flags/docker"
	"github.com/nicholas-fedor/watchtower/internal/flags/filter"
	"github.com/nicholas-fedor/watchtower/internal/flags/lifecycle"
	"github.com/nicholas-fedor/watchtower/internal/flags/logging"
	"github.com/nicholas-fedor/watchtower/internal/flags/mode"
	"github.com/nicholas-fedor/watchtower/internal/flags/notify"
	"github.com/nicholas-fedor/watchtower/internal/flags/registry"
	"github.com/nicholas-fedor/watchtower/internal/flags/schedule"
	"github.com/nicholas-fedor/watchtower/internal/flags/update"
	"github.com/nicholas-fedor/watchtower/internal/flags/utils"
)

// DockerAPIMinVersion sets the minimum Docker API version supported by Watchtower.
const DockerAPIMinVersion string = "1.24"

// Errors for flag and environment configuration.
var (
	// errInvalidLogFormat indicates an invalid log format was specified in configuration.
	errInvalidLogFormat = errors.New("invalid log format specified")
	// errInvalidLogLevel indicates an invalid log level was specified in configuration.
	errInvalidLogLevel = errors.New("invalid log level specified")
	// errSetEnvFailed indicates a failure to set an environment variable during configuration.
	errSetEnvFailed = errors.New("failed to set environment variable")
	// errOpenFileFailed indicates a failure to open a file when reading secrets.
	errOpenFileFailed = errors.New("failed to open secret file")
	// errReplaceSliceFailed indicates a failure to replace a slice value in a flag.
	errReplaceSliceFailed = errors.New("failed to replace slice value in flag")
	// errReadFileFailed indicates a failure to read a file's contents for secrets.
	errReadFileFailed = errors.New("failed to read secret file")
	// errInvalidSecretURL indicates an invalid URL was found in a secret file.
	errInvalidSecretURL = errors.New("invalid notification URL in secret file")
	// errSetFlagFailed indicates a failure to set a flag's value during configuration.
	errSetFlagFailed = errors.New("failed to set flag value")
	// errInvalidFlagName indicates an invalid flag name was provided for modification.
	errInvalidFlagName = errors.New("invalid flag name provided")
	// errNotSliceValue indicates a flag does not support slice values for appending.
	errNotSliceValue = errors.New("flag does not support slice values")
)

// RegisterDockerFlags adds Docker API client flags to the root command.
//
// Prefer RegisterAll when registering the full flag set.
//
// Parameters:
//   - rootCmd: Root Cobra command.
func RegisterDockerFlags(rootCmd *cobra.Command) {
	docker.Register(rootCmd)
}

// RegisterSystemFlags registers non-Docker, non-notification domain flags.
//
// Prefer RegisterAll. Kept for tests that call domain groups separately.
//
// Parameters:
//   - rootCmd: Root Cobra command.
func RegisterSystemFlags(rootCmd *cobra.Command) {
	client.Register(rootCmd)
	schedule.Register(rootCmd)
	mode.Register(rootCmd)
	update.Register(rootCmd)
	lifecycle.Register(rootCmd)
	filter.Register(rootCmd)
	registry.Register(rootCmd)
	compat.Register(rootCmd)
	api.Register(rootCmd)
	logging.Register(rootCmd)
}

// RegisterNotificationFlags adds notification flags to the root command.
//
// Prefer RegisterAll when registering the full flag set.
//
// Parameters:
//   - rootCmd: Root Cobra command.
func RegisterNotificationFlags(rootCmd *cobra.Command) {
	notify.Register(rootCmd)
}

// filterEmptyStrings is a package-local alias for utils.FilterEmptyStrings (tests).
func filterEmptyStrings(values []string) []string {
	return utils.FilterEmptyStrings(values)
}

// isPureNumeric reports whether str is a bare number (tests and env duration helpers).
func isPureNumeric(str string) bool {
	return utils.IsPureNumeric(str)
}

// SetDefaults enables automatic environment lookup on the global Viper instance.
//
// Flag static defaults and process Load bind live on FlagSpec rows. This remains
// for tests and helpers that still touch the global Viper (for example EnvDuration).
func SetDefaults() {
	viper.AutomaticEnv()
}

// EnvConfig sets Docker environment variables from flags.
//
// Parameters:
//   - cmd: Cobra command with flags.
//
// Returns:
//   - error: Non-nil if flag retrieval fails, nil on success.
func EnvConfig(cmd *cobra.Command) error {
	flagSet := cmd.PersistentFlags()

	// Resolve Docker settings via Viper (flag > env > static default) after BindAll.
	vip := viper.New()

	err := BindAll(vip, flagSet, docker.Specs())
	if err != nil {
		return fmt.Errorf("bind docker flags: %w", err)
	}

	host := vip.GetString("host")
	tls := vip.GetBool("tlsverify")
	version := strings.Trim(vip.GetString("api-version"), "\"")
	certPath := vip.GetString("cert-path")

	// Convert tcp:// to https:// when TLS is enabled.
	if tls && strings.HasPrefix(host, "tcp://") {
		host = strings.Replace(host, "tcp://", "https://", 1)
	}

	// Warn about mismatched TLS settings.
	if tls {
		if strings.HasPrefix(host, "http://") {
			logrus.Warn(
				"TLS verification is enabled but DOCKER_HOST uses insecure scheme 'http://'. Consider using 'https://' or disable TLS verification.",
			)
		} else if strings.HasPrefix(host, "unix://") {
			logrus.Warn(
				"TLS verification is enabled but DOCKER_HOST uses local socket 'unix://'. TLS is not applicable for local sockets; consider disabling TLS verification.",
			)
		}
	}

	// Set environment variables.
	err = setEnvOptStr("DOCKER_HOST", host)
	if err != nil {
		return err
	}

	err = setEnvOptBool("DOCKER_TLS_VERIFY", tls)
	if err != nil {
		return err
	}

	err = setEnvOptStr("DOCKER_API_VERSION", version)
	if err != nil {
		return err
	}

	err = setEnvOptStr("DOCKER_CERT_PATH", certPath)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"host":     host,
		"tls":      tls,
		"version":  version,
		"certPath": certPath,
	}).Debug("Configured Docker environment variables")

	return nil
}

// setEnvOptStr sets an environment variable if needed.
//
// Parameters:
//   - env: Environment variable name.
//   - opt: Value to set.
//
// Returns:
//   - error: Non-nil if set fails, nil if skipped or successful.
func setEnvOptStr(env, opt string) error {
	if opt == "" || opt == os.Getenv(env) {
		return nil
	}

	err := os.Setenv(env, opt)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"env":   env,
			"value": opt,
		}).Debug("Failed to set environment variable")

		return fmt.Errorf("%w: %s: %w", errSetEnvFailed, env, err)
	}

	logrus.WithFields(logrus.Fields{
		"env":   env,
		"value": opt,
	}).Debug("Set environment variable")

	return nil
}

// setEnvOptBool sets an environment variable to "1" if true.
//
// Parameters:
//   - env: Environment variable name.
//   - opt: Boolean value.
//
// Returns:
//   - error: Non-nil if set fails, nil otherwise.
func setEnvOptBool(env string, opt bool) error {
	if opt {
		return setEnvOptStr(env, "1")
	}

	return nil
}

// GetSecretsFromFiles updates flags with file contents for secrets.
//
// Parameters:
//   - rootCmd: Root Cobra command.
//
//nolint:godox
func GetSecretsFromFiles(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()
	secrets := []string{
		// TODO: Remove just before v2 Release.
		"notification-email-server-password",
		// TODO: Remove just before v2 Release.
		"notification-slack-hook-url",
		// TODO: Remove just before v2 Release.
		"notification-msteams-hook",
		// TODO: Remove just before v2 Release.
		"notification-gotify-token",
		"notification-url",
		"http-api-token",
		"http-api-events-token",
	}

	// Process each secret flag.
	for _, secret := range secrets {
		err := getSecretFromFile(flags, secret)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"flag": secret,
			}).Fatal("Failed to load secret from file")
		}
	}
}

// getSecretFromFile reads file contents into a flag if applicable.
//
// Parameters:
//   - flags: Flag set.
//   - secret: Flag name.
//
// Returns:
//   - error: Non-nil if file ops fail, nil on success or skip.
func getSecretFromFile(flags *pflag.FlagSet, secret string) error {
	flag := flags.Lookup(secret)
	fields := logrus.Fields{"flag": secret}

	// Handle slice flags.
	if sliceValue, ok := flag.Value.(pflag.SliceValue); ok {
		oldValues := sliceValue.GetSlice()
		values := make([]string, 0, len(oldValues))

		for _, value := range oldValues {
			if value != "" && isFilePath(value) {
				file, err := os.Open(value)
				if err != nil {
					logrus.WithError(err).WithFields(fields).
						WithField("file", value).
						Debug("Failed to open secret file")

					return fmt.Errorf("%w: %w", errOpenFileFailed, err)
				}

				defer func() { _ = file.Close() }()

				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}

					if secret == "notification-url" {
						if !strings.Contains(line, "://") {
							return errInvalidSecretURL
						}

						parsedURL, err := url.Parse(line)
						if err != nil || parsedURL.Scheme == "" {
							return errInvalidSecretURL
						}

						if parsedURL.Opaque == "" && parsedURL.Host == "" && parsedURL.Path == "" {
							if parsedURL.Scheme != "logger" && parsedURL.Scheme != "mock" {
								return errInvalidSecretURL
							}
						}
					}

					values = append(values, line)
				}

				err = scanner.Err()
				if err != nil {
					logrus.WithFields(fields).
						WithField("file", value).
						WithError(err).
						Debug("Failed to read secret file")

					return fmt.Errorf("%w: %w", errReadFileFailed, err)
				}

				logrus.WithFields(fields).
					WithField("file", value).
					Debug("Read secret from file into slice")
			} else {
				values = append(values, value)
			}
		}

		err := sliceValue.Replace(values)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to replace slice value in flag")

			return fmt.Errorf("%w: %w", errReplaceSliceFailed, err)
		}

		// Mark the flag as explicitly set so downstream consumers read the expanded
		// value from the flag rather than re-deriving from raw os.Getenv.
		flag.Changed = true

		return nil
	}

	// Handle string flags.
	value := flag.Value.String()
	if value != "" && isFilePath(value) {
		content, err := os.ReadFile(value)
		if err != nil {
			logrus.WithFields(fields).
				WithField("file", value).
				WithError(err).
				Debug("Failed to read secret file")

			return fmt.Errorf("%w: %w", errReadFileFailed, err)
		}

		err = flags.Set(secret, strings.TrimSpace(string(content)))
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to set flag from file contents")

			return fmt.Errorf("%w: %w", errSetFlagFailed, err)
		}

		logrus.WithFields(fields).WithField("file", value).Debug("Set flag from file contents")
	}

	return nil
}

// isFilePath checks if a string is likely a file path.
//
// Parameters:
//   - path: String to check.
//
// Returns:
//   - bool: True if likely a file path, false otherwise.
func isFilePath(path string) bool {
	firstColon := strings.IndexRune(path, ':')
	if firstColon != 1 && firstColon != -1 {
		// If ':' exists but isn't the second character, it's likely not a file path (e.g., URLs).
		return false
	}

	//nolint:gosec // G703: Path traversal via taint analysis - validating user-provided path exists
	_, err := os.Stat(path)

	return !errors.Is(err, os.ErrNotExist)
}

// ProcessFlagAliases applies environment values then syncs flag aliases.
//
// It bridges env onto unset flags, then applies porcelain mode, interval versus
// schedule conflicts, and debug/trace log-level forcing. Call after Cobra parse
// and before SetupLogging / secrets expansion / config.Load.
//
// Parameters:
//   - flags: Parsed persistent flag set.
func ProcessFlagAliases(flags *pflag.FlagSet) {
	// Ensure env-sourced values are visible to alias logic via flag Gets.
	err := ApplyEnvToFlags(flags, AllSpecs())
	if err != nil {
		logrus.WithError(err).Fatal("Failed to apply environment configuration")
	}

	// Handle porcelain mode.
	porcelain, err := flags.GetString("porcelain")
	if err != nil {
		logrus.WithField("flag", "porcelain").
			WithError(err).
			Fatal("Failed to get porcelain flag")
	}

	if porcelain != "" {
		if porcelain != "v1" {
			logrus.WithField("version", porcelain).Fatal("Unknown porcelain version, supported: v1")
		}

		err := appendFlagValue(flags, "notification-url", "logger://")
		if err != nil {
			logrus.WithError(err).Debug("Failed to append notification-url")
		}

		setFlagIfDefault(flags, "notification-log-stdout", "true")
		setFlagIfDefault(flags, "notification-report", "true")

		tpl := fmt.Sprintf("porcelain.%s.summary-no-log", porcelain)
		setFlagIfDefault(flags, "notification-template", tpl)
		logrus.WithField("porcelain", porcelain).Debug("Configured porcelain mode")
	}

	// Handle interval vs. schedule conflicts.
	scheduleChanged := flags.Changed("schedule")
	intervalChanged := flags.Changed("interval")

	if val, _ := flags.GetString("schedule"); val != "" {
		scheduleChanged = true
	}

	if val, _ := flags.GetInt("interval"); val != schedule.DefaultPollIntervalSeconds {
		intervalChanged = true
	}

	if intervalChanged && scheduleChanged {
		logrus.WithFields(logrus.Fields{
			"interval": intervalChanged,
			"schedule": scheduleChanged,
		}).Fatal("Cannot define both interval and schedule")
	}

	// Update schedule to match interval or default if needed.
	if intervalChanged || !scheduleChanged {
		interval, _ := flags.GetInt("interval")

		scheduleValue := fmt.Sprintf("@every %ds", interval)

		err := flags.Set("schedule", scheduleValue)
		if err != nil {
			logrus.WithError(err).
				WithField("interval", interval).
				Debug("Failed to set schedule from interval")
		} else {
			logrus.WithFields(logrus.Fields{
				"interval": interval,
				"schedule": scheduleValue,
			}).Debug("Set default schedule from interval")
		}
	}

	// Adjust log level for debug/trace.
	if flagIsEnabled(flags, "debug") {
		err := flags.Set("log-level", "debug")
		if err != nil {
			logrus.WithError(err).Debug("Failed to set debug log level")
		}
	}

	if flagIsEnabled(flags, "trace") {
		err := flags.Set("log-level", "trace")
		if err != nil {
			logrus.WithError(err).Debug("Failed to set trace log level")
		}
	}
}

// SetupLogging configures the global logger.
//
// Parameters:
//   - flags: Flag set.
//
// Returns:
//   - error: Non-nil if config fails, nil on success.
func SetupLogging(flags *pflag.FlagSet) error {
	logFormat, err := flags.GetString("log-format")
	if err != nil {
		logrus.WithField("flag", "log-format").WithError(err).Debug("Failed to get log-format flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	// Default to "auto" when neither the flag nor WATCHTOWER_LOG_FORMAT is set.
	// This prevents configureLogFormat from returning errInvalidLogFormat on empty strings,
	// which is the case when running the ephemeral orchestrator container without
	// WATCHTOWER_LOG_FORMAT in its environment.
	if logFormat == "" {
		logFormat = "auto"
	}

	noColor, err := flags.GetBool("no-color")
	if err != nil {
		logrus.WithField("flag", "no-color").WithError(err).Debug("Failed to get no-color flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	err = configureLogFormat(logFormat, noColor)
	if err != nil {
		return err
	}

	// Set log level only when explicitly specified.
	rawLogLevel, err := flags.GetString("log-level")
	if err != nil {
		logrus.WithField("flag", "log-level").WithError(err).Debug("Failed to get log-level flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	// Only parse and override the log level when a value was explicitly set.
	// When rawLogLevel is empty (neither --log-level nor WATCHTOWER_LOG_LEVEL is set),
	// preserve the level configured earlier (e.g., InfoLevel from main.go init).
	// This prevents logrus.ParseLevel("") from returning an error, which would
	// cause SetupLogging to fail and preRun to call logrus.Fatal before the
	// orchestrator mode check — silently killing the ephemeral orchestrator container.
	if rawLogLevel != "" {
		logLevel, err := logrus.ParseLevel(rawLogLevel)
		if err != nil {
			logrus.WithError(err).WithField("level", rawLogLevel).Debug("Invalid log level specified")

			return fmt.Errorf("%w: %w", errInvalidLogLevel, err)
		}

		logrus.SetLevel(logLevel)
	}

	logrus.WithFields(logrus.Fields{
		"format":   logFormat,
		"level":    logrus.GetLevel(),
		"no_color": noColor,
	}).Debug("Configured logging settings")

	return nil
}

// configureLogFormat sets the logrus formatter.
//
// Parameters:
//   - logFormat: Desired format.
//   - noColor: Disable colors if true.
//
// Returns:
//   - error: Non-nil if format invalid, nil on success.
func configureLogFormat(logFormat string, noColor bool) error {
	switch strings.ToLower(logFormat) {
	case "auto":
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors:             noColor,
			EnvironmentOverrideColors: true,
		})
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{})
	case "logfmt":
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors: true,
			FullTimestamp: true,
		})
	case "pretty":
		logrus.SetFormatter(&logrus.TextFormatter{
			ForceColors:   !noColor,
			FullTimestamp: false,
		})
	default:
		logrus.WithField("format", logFormat).Debug("Invalid log format specified")

		return fmt.Errorf("%w: %s", errInvalidLogFormat, logFormat)
	}

	return nil
}

// flagIsEnabled checks if a boolean flag is true.
//
// Parameters:
//   - flags: Flag set.
//   - name: Flag name.
//
// Returns:
//   - bool: True if enabled.
func flagIsEnabled(flags *pflag.FlagSet, name string) bool {
	value, err := flags.GetBool(name)
	if err != nil {
		logrus.WithField("flag", name).WithError(err).Fatal("Failed to check flag status")
	}

	return value
}

// appendFlagValue appends values to a slice flag.
//
// Parameters:
//   - flags: Flag set.
//   - name: Flag name.
//   - values: Values to append.
//
// Returns:
//   - error: Non-nil if append fails, nil on success.
func appendFlagValue(flags *pflag.FlagSet, name string, values ...string) error {
	flag := flags.Lookup(name)
	if flag == nil {
		logrus.WithField("flag", name).Debug("Invalid flag name provided")

		return fmt.Errorf("%w: %q", errInvalidFlagName, name)
	}

	if flagValues, ok := flag.Value.(pflag.SliceValue); ok {
		for _, value := range values {
			err := flagValues.Append(value)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"flag":  name,
					"value": value,
				}).Debug("Failed to append value to flag")
			}
		}
	} else {
		logrus.WithField("flag", name).Debug("Flag does not support slice values")

		return fmt.Errorf("%w: %q", errNotSliceValue, name)
	}

	return nil
}

// setFlagIfDefault sets a flag's default value if unchanged.
//
// Parameters:
//   - flags: Flag set.
//   - name: Flag name.
//   - value: Default value.
func setFlagIfDefault(flags *pflag.FlagSet, name, value string) {
	if flags.Changed(name) {
		return
	}

	err := flags.Set(name, value)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"flag":  name,
			"value": value,
			"error": err,
		}).Debug("Failed to set default flag value")
	} else {
		logrus.WithFields(logrus.Fields{
			"flag":  name,
			"value": value,
		}).Debug("Set default flag value")
	}
}
