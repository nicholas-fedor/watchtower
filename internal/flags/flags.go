// Package flags manages command-line flags and environment variables for Watchtower configuration.
package flags

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// DockerAPIMinVersion specifies the minimum Docker API version required by Watchtower.
// It ensures compatibility with the Docker client.
const DockerAPIMinVersion string = "1.44"

// defaultPollIntervalSeconds defines the default polling interval in seconds (24 hours).
// It provides a consistent default for periodic updates.
const defaultPollIntervalSeconds = 86400 // 24 * 60 * 60 seconds

// defaultStopTimeoutSeconds defines the default timeout for stopping containers (10 seconds).
// It ensures a consistent default for container stop operations.
const defaultStopTimeoutSeconds = 10

// defaultEmailServerPort defines the default SMTP server port for email notifications (25).
// It provides a standard port for email communication.
const defaultEmailServerPort = 25

// errInvalidLogFormat indicates an invalid log format was specified.
// It is used in SetupLogging to report configuration errors.
var errInvalidLogFormat = errors.New("invalid log format specified")

// errInvalidLogLevel indicates an invalid log level was specified.
// It is used in SetupLogging to report configuration errors.
var errInvalidLogLevel = errors.New("invalid log level specified")

// errSetEnvFailed indicates a failure to set an environment variable.
// It is used in setEnvOptStr to wrap os.Setenv errors.
var errSetEnvFailed = errors.New("failed to set environment variable")

// errOpenFileFailed indicates a failure to open a file for reading secrets.
// It is used in getSecretFromFile to wrap os.Open errors.
var errOpenFileFailed = errors.New("failed to open secret file")

// errCloseFileFailed indicates a failure to close a file after reading secrets.
// It is used in getSecretFromFile to wrap file.Close errors.
var errCloseFileFailed = errors.New("failed to close secret file")

// errReplaceSliceFailed indicates a failure to replace a slice value in a flag.
// It is used in getSecretFromFile to wrap SliceValue.Replace errors.
var errReplaceSliceFailed = errors.New("failed to replace slice value in flag")

// errReadFileFailed indicates a failure to read a file’s contents.
// It is used in getSecretFromFile to wrap os.ReadFile errors.
var errReadFileFailed = errors.New("failed to read secret file")

// errSetFlagFailed indicates a failure to set a flag’s value.
// It is used in getSecretFromFile and setFlagIfDefault to wrap flags.Set errors.
var errSetFlagFailed = errors.New("failed to set flag value")

// errInvalidFlagName indicates an invalid flag name was provided.
// It is used in appendFlagValue to report flag lookup errors.
var errInvalidFlagName = errors.New("invalid flag name provided")

// errNotSliceValue indicates a flag does not support slice values.
// It is used in appendFlagValue to report type errors.
var errNotSliceValue = errors.New("flag does not support slice values")

// RegisterDockerFlags adds flags used directly by the Docker API client to the root command.
// These flags configure the Docker connection settings.
func RegisterDockerFlags(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()
	flags.StringP("host", "H", envString("DOCKER_HOST"), "daemon socket to connect to")
	flags.BoolP("tlsverify", "v", envBool("DOCKER_TLS_VERIFY"), "use TLS and verify the remote")
	flags.StringP(
		"api-version",
		"a",
		envString("DOCKER_API_VERSION"),
		"api version to use by docker client",
	)
}

// RegisterSystemFlags adds flags that modify Watchtower’s program flow to the root command.
// These flags control update behavior, logging, and operational modes.
func RegisterSystemFlags(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()
	flags.IntP(
		"interval",
		"i",
		envInt("WATCHTOWER_POLL_INTERVAL"),
		"Poll interval (in seconds)")

	flags.StringP(
		"schedule",
		"s",
		envString("WATCHTOWER_SCHEDULE"),
		"The cron expression which defines when to update")

	flags.DurationP(
		"stop-timeout",
		"t",
		envDuration("WATCHTOWER_TIMEOUT"),
		"Timeout before a container is forcefully stopped")

	flags.BoolP(
		"no-pull",
		"",
		envBool("WATCHTOWER_NO_PULL"),
		"Do not pull any new images")

	flags.BoolP(
		"no-restart",
		"",
		envBool("WATCHTOWER_NO_RESTART"),
		"Do not restart any containers")

	flags.BoolP(
		"no-startup-message",
		"",
		envBool("WATCHTOWER_NO_STARTUP_MESSAGE"),
		"Prevents watchtower from sending a startup message")

	flags.BoolP(
		"cleanup",
		"c",
		envBool("WATCHTOWER_CLEANUP"),
		"Remove previously used images after updating")

	flags.BoolP(
		"remove-volumes",
		"",
		envBool("WATCHTOWER_REMOVE_VOLUMES"),
		"Remove attached volumes before updating")

	flags.BoolP(
		"label-enable",
		"e",
		envBool("WATCHTOWER_LABEL_ENABLE"),
		"Watch containers where the com.centurylinklabs.watchtower.enable label is true")

	flags.StringSliceP(
		"disable-containers",
		"x",
		// Due to issue spf13/viper#380, can't use viper.GetStringSlice:
		regexp.MustCompile("[, ]+").Split(envString("WATCHTOWER_DISABLE_CONTAINERS"), -1),
		"Comma-separated list of containers to explicitly exclude from watching.")

	flags.StringP(
		"log-format",
		"l",
		viper.GetString("WATCHTOWER_LOG_FORMAT"),
		"Sets what logging format to use for console output. Possible values: Auto, LogFmt, Pretty, JSON",
	)

	flags.BoolP(
		"debug",
		"d",
		envBool("WATCHTOWER_DEBUG"),
		"Enable debug mode with verbose logging")

	flags.BoolP(
		"trace",
		"",
		envBool("WATCHTOWER_TRACE"),
		"Enable trace mode with very verbose logging - caution, exposes credentials")

	flags.BoolP(
		"monitor-only",
		"m",
		envBool("WATCHTOWER_MONITOR_ONLY"),
		"Will only monitor for new images, not update the containers")

	flags.BoolP(
		"run-once",
		"R",
		envBool("WATCHTOWER_RUN_ONCE"),
		"Run once now and exit")

	flags.BoolP(
		"include-restarting",
		"",
		envBool("WATCHTOWER_INCLUDE_RESTARTING"),
		"Will also include restarting containers")

	flags.BoolP(
		"include-stopped",
		"S",
		envBool("WATCHTOWER_INCLUDE_STOPPED"),
		"Will also include created and exited containers")

	flags.BoolP(
		"revive-stopped",
		"",
		envBool("WATCHTOWER_REVIVE_STOPPED"),
		"Will also start stopped containers that were updated, if include-stopped is active")

	flags.BoolP(
		"enable-lifecycle-hooks",
		"",
		envBool("WATCHTOWER_LIFECYCLE_HOOKS"),
		"Enable the execution of commands triggered by pre- and post-update lifecycle hooks")

	flags.BoolP(
		"rolling-restart",
		"",
		envBool("WATCHTOWER_ROLLING_RESTART"),
		"Restart containers one at a time")

	flags.BoolP(
		"http-api-update",
		"",
		envBool("WATCHTOWER_HTTP_API_UPDATE"),
		"Runs Watchtower in HTTP API mode, so that image updates must to be triggered by a request")
	flags.BoolP(
		"http-api-metrics",
		"",
		envBool("WATCHTOWER_HTTP_API_METRICS"),
		"Runs Watchtower with the Prometheus metrics API enabled")

	flags.StringP(
		"http-api-port",
		"",
		envString("WATCHTOWER_HTTP_API_PORT"),
		"Port to bind the HTTP API to (default: 8080)")

	flags.StringP(
		"http-api-token",
		"",
		envString("WATCHTOWER_HTTP_API_TOKEN"),
		"Sets an authentication token to HTTP API requests.")

	flags.BoolP(
		"http-api-periodic-polls",
		"",
		envBool("WATCHTOWER_HTTP_API_PERIODIC_POLLS"),
		"Also run periodic updates (specified with --interval and --schedule) if HTTP API is enabled",
	)

	// https://no-color.org/
	flags.BoolP(
		"no-color",
		"",
		viper.IsSet("NO_COLOR"),
		"Disable ANSI color escape codes in log output")

	flags.StringP(
		"scope",
		"",
		envString("WATCHTOWER_SCOPE"),
		"Defines a monitoring scope for the Watchtower instance.")

	flags.StringP(
		"porcelain",
		"P",
		envString("WATCHTOWER_PORCELAIN"),
		`Write session results to stdout using a stable versioned format. Supported values: "v1"`)

	flags.String(
		"log-level",
		envString("WATCHTOWER_LOG_LEVEL"),
		"The maximum log level that will be written to STDERR. Possible values: panic, fatal, error, warn, info, debug or trace",
	)

	flags.BoolP(
		"health-check",
		"",
		false,
		"Do health check and exit")

	flags.BoolP(
		"label-take-precedence",
		"",
		envBool("WATCHTOWER_LABEL_TAKE_PRECEDENCE"),
		"Label applied to containers take precedence over arguments")
}

// RegisterNotificationFlags adds flags for configuring Watchtower notifications to the root command.
// These flags control how and when notifications are sent.
func RegisterNotificationFlags(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()

	flags.StringSliceP(
		"notifications",
		"n",
		envStringSlice("WATCHTOWER_NOTIFICATIONS"),
		"Notification types to send (valid: email, slack, msteams, gotify, shoutrrr)")

	flags.String(
		"notifications-level",
		envString("WATCHTOWER_NOTIFICATIONS_LEVEL"),
		"The log level used for sending notifications. Possible values: panic, fatal, error, warn, info or debug",
	)

	flags.IntP(
		"notifications-delay",
		"",
		envInt("WATCHTOWER_NOTIFICATIONS_DELAY"),
		"Delay before sending notifications, expressed in seconds")

	flags.StringP(
		"notifications-hostname",
		"",
		envString("WATCHTOWER_NOTIFICATIONS_HOSTNAME"),
		"Custom hostname for notification titles")

	flags.StringP(
		"notification-email-from",
		"",
		envString("WATCHTOWER_NOTIFICATION_EMAIL_FROM"),
		"Address to send notification emails from")

	flags.StringP(
		"notification-email-to",
		"",
		envString("WATCHTOWER_NOTIFICATION_EMAIL_TO"),
		"Address to send notification emails to")

	flags.IntP(
		"notification-email-delay",
		"",
		envInt("WATCHTOWER_NOTIFICATION_EMAIL_DELAY"),
		"Delay before sending notifications, expressed in seconds")

	flags.StringP(
		"notification-email-server",
		"",
		envString("WATCHTOWER_NOTIFICATION_EMAIL_SERVER"),
		"SMTP server to send notification emails through")

	flags.IntP(
		"notification-email-server-port",
		"",
		envInt("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT"),
		"SMTP server port to send notification emails through")

	flags.BoolP(
		"notification-email-server-tls-skip-verify",
		"",
		envBool("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_TLS_SKIP_VERIFY"),
		"Controls whether watchtower verifies the SMTP server's certificate chain and host name. Should only be used for testing.",
	)

	flags.StringP(
		"notification-email-server-user",
		"",
		envString("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER"),
		"SMTP server user for sending notifications")

	flags.StringP(
		"notification-email-server-password",
		"",
		envString("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD"),
		"SMTP server password for sending notifications")

	flags.StringP(
		"notification-email-subjecttag",
		"",
		envString("WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG"),
		"Subject prefix tag for notifications via mail")

	flags.StringP(
		"notification-slack-hook-url",
		"",
		envString("WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL"),
		"The Slack Hook URL to send notifications to")

	flags.StringP(
		"notification-slack-identifier",
		"",
		envString("WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER"),
		"A string which will be used to identify the messages coming from this watchtower instance")

	flags.StringP(
		"notification-slack-channel",
		"",
		envString("WATCHTOWER_NOTIFICATION_SLACK_CHANNEL"),
		"A string which overrides the webhook's default channel. Example: #my-custom-channel")

	flags.StringP(
		"notification-slack-icon-emoji",
		"",
		envString("WATCHTOWER_NOTIFICATION_SLACK_ICON_EMOJI"),
		"An emoji code string to use in place of the default icon")

	flags.StringP(
		"notification-slack-icon-url",
		"",
		envString("WATCHTOWER_NOTIFICATION_SLACK_ICON_URL"),
		"An icon image URL string to use in place of the default icon")

	flags.StringP(
		"notification-msteams-hook",
		"",
		envString("WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL"),
		"The MSTeams WebHook URL to send notifications to")

	flags.BoolP(
		"notification-msteams-data",
		"",
		envBool("WATCHTOWER_NOTIFICATION_MSTEAMS_USE_LOG_DATA"),
		"The MSTeams notifier will try to extract log entry fields as MSTeams message facts")

	flags.StringP(
		"notification-gotify-url",
		"",
		envString("WATCHTOWER_NOTIFICATION_GOTIFY_URL"),
		"The Gotify URL to send notifications to")

	flags.StringP(
		"notification-gotify-token",
		"",
		envString("WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN"),
		"The Gotify Application required to query the Gotify API")

	flags.BoolP(
		"notification-gotify-tls-skip-verify",
		"",
		envBool("WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY"),
		"Controls whether watchtower verifies the Gotify server's certificate chain and host name. Should only be used for testing.",
	)

	flags.String(
		"notification-template",
		envString("WATCHTOWER_NOTIFICATION_TEMPLATE"),
		"The shoutrrr text/template for the messages")

	flags.StringArray(
		"notification-url",
		envStringSlice("WATCHTOWER_NOTIFICATION_URL"),
		"The shoutrrr URL to send notifications to")

	flags.Bool("notification-report",
		envBool("WATCHTOWER_NOTIFICATION_REPORT"),
		"Use the session report as the notification template data")

	flags.StringP(
		"notification-title-tag",
		"",
		envString("WATCHTOWER_NOTIFICATION_TITLE_TAG"),
		"Title prefix tag for notifications")

	flags.Bool("notification-skip-title",
		envBool("WATCHTOWER_NOTIFICATION_SKIP_TITLE"),
		"Do not pass the title param to notifications")

	flags.String(
		"warn-on-head-failure",
		envString("WATCHTOWER_WARN_ON_HEAD_FAILURE"),
		"When to warn about HEAD pull requests failing. Possible values: always, auto or never")

	flags.Bool(
		"notification-log-stdout",
		envBool("WATCHTOWER_NOTIFICATION_LOG_STDOUT"),
		"Write notification logs to stdout instead of logging (to stderr)")
}

// envString retrieves a string value from an environment variable via Viper.
// It binds the key to the environment and returns its value.
func envString(key string) string {
	viper.MustBindEnv(key)

	return viper.GetString(key)
}

// envStringSlice retrieves a string slice from an environment variable via Viper.
// It binds the key to the environment and returns its values.
func envStringSlice(key string) []string {
	viper.MustBindEnv(key)

	return viper.GetStringSlice(key)
}

// envInt retrieves an integer value from an environment variable via Viper.
// It binds the key to the environment and returns its value.
func envInt(key string) int {
	viper.MustBindEnv(key)

	return viper.GetInt(key)
}

// envBool retrieves a boolean value from an environment variable via Viper.
// It binds the key to the environment and returns its value.
func envBool(key string) bool {
	viper.MustBindEnv(key)

	return viper.GetBool(key)
}

// envDuration retrieves a duration value from an environment variable via Viper.
// It binds the key to the environment and returns its value.
func envDuration(key string) time.Duration {
	viper.MustBindEnv(key)

	return viper.GetDuration(key)
}

// SetDefaults configures default values for environment variables.
// It ensures consistent fallback behavior when flags or environment variables are unset.
func SetDefaults() {
	viper.AutomaticEnv()
	viper.SetDefault("DOCKER_HOST", "unix:///var/run/docker.sock")
	viper.SetDefault("DOCKER_API_VERSION", DockerAPIMinVersion)
	viper.SetDefault("WATCHTOWER_POLL_INTERVAL", defaultPollIntervalSeconds)
	viper.SetDefault("WATCHTOWER_TIMEOUT", time.Second*defaultStopTimeoutSeconds)
	viper.SetDefault("WATCHTOWER_HTTP_API_PORT", "8080")
	viper.SetDefault("WATCHTOWER_NOTIFICATIONS", []string{})
	viper.SetDefault("WATCHTOWER_NOTIFICATIONS_LEVEL", "info")
	viper.SetDefault("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT", defaultEmailServerPort)
	viper.SetDefault("WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG", "")
	viper.SetDefault("WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER", "watchtower")
	viper.SetDefault("WATCHTOWER_LOG_LEVEL", "info")
	viper.SetDefault("WATCHTOWER_LOG_FORMAT", "auto")
}

// EnvConfig sets environment variables based on Docker-related flags.
// It configures the Docker client’s environment, returning an error if flag retrieval fails.
func EnvConfig(cmd *cobra.Command) error {
	var err error

	var host string

	var tls bool

	var version string

	flags := cmd.PersistentFlags()

	if host, err = flags.GetString("host"); err != nil {
		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	if tls, err = flags.GetBool("tlsverify"); err != nil {
		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	if version, err = flags.GetString("api-version"); err != nil {
		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	if err = setEnvOptStr("DOCKER_HOST", host); err != nil {
		return err
	}

	if err = setEnvOptBool("DOCKER_TLS_VERIFY", tls); err != nil {
		return err
	}

	if err = setEnvOptStr("DOCKER_API_VERSION", version); err != nil {
		return err
	}

	return nil
}

// ReadFlags retrieves common operational flags used in Watchtower’s main flow.
// It returns cleanup, noRestart, monitorOnly, and timeout values, exiting on error.
func ReadFlags(cmd *cobra.Command) (bool, bool, bool, time.Duration) {
	flags := cmd.PersistentFlags()

	var err error

	var cleanup bool

	var noRestart bool

	var monitorOnly bool

	var timeout time.Duration

	if cleanup, err = flags.GetBool("cleanup"); err != nil {
		logrus.Fatal(err)
	}

	if noRestart, err = flags.GetBool("no-restart"); err != nil {
		logrus.Fatal(err)
	}

	if monitorOnly, err = flags.GetBool("monitor-only"); err != nil {
		logrus.Fatal(err)
	}

	if timeout, err = flags.GetDuration("stop-timeout"); err != nil {
		logrus.Fatal(err)
	}

	return cleanup, noRestart, monitorOnly, timeout
}

// setEnvOptStr sets an environment variable to a specified string value if needed.
// It skips setting if the value is empty or matches the current environment, returning an error if the set fails.
func setEnvOptStr(env string, opt string) error {
	if opt == "" || opt == os.Getenv(env) {
		return nil
	}

	if err := os.Setenv(env, opt); err != nil {
		return fmt.Errorf("%w: %s: %w", errSetEnvFailed, env, err)
	}

	return nil
}

// setEnvOptBool sets an environment variable to "1" if the boolean is true.
// It returns an error if the set operation fails, otherwise nil.
func setEnvOptBool(env string, opt bool) error {
	if opt {
		return setEnvOptStr(env, "1")
	}

	return nil
}

// GetSecretsFromFiles replaces flag values with file contents if they reference files.
// It processes a predefined list of secret-related flags, updating their values accordingly.
func GetSecretsFromFiles(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()

	secrets := []string{
		"notification-email-server-password",
		"notification-slack-hook-url",
		"notification-msteams-hook",
		"notification-gotify-token",
		"notification-url",
		"http-api-token",
	}
	for _, secret := range secrets {
		if err := getSecretFromFile(flags, secret); err != nil {
			logrus.Fatalf("failed to get secret from flag %v: %s", secret, err)
		}
	}
}

// getSecretFromFile updates a flag’s value with file contents if it references a file.
// It handles both string and slice flags, returning an error if file operations fail.
func getSecretFromFile(flags *pflag.FlagSet, secret string) error {
	flag := flags.Lookup(secret)
	if sliceValue, ok := flag.Value.(pflag.SliceValue); ok {
		oldValues := sliceValue.GetSlice()
		values := make([]string, 0, len(oldValues))

		for _, value := range oldValues {
			if value != "" && isFilePath(value) {
				file, err := os.Open(value)
				if err != nil {
					return fmt.Errorf("%w: %w", errOpenFileFailed, err)
				}

				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "" {
						continue
					}

					values = append(values, line)
				}

				if err := file.Close(); err != nil {
					return fmt.Errorf("%w: %w", errCloseFileFailed, err)
				}
			} else {
				values = append(values, value)
			}
		}

		if err := sliceValue.Replace(values); err != nil {
			return fmt.Errorf("%w: %w", errReplaceSliceFailed, err)
		}

		return nil
	}

	value := flag.Value.String()
	if value != "" && isFilePath(value) {
		content, err := os.ReadFile(value)
		if err != nil {
			return fmt.Errorf("%w: %w", errReadFileFailed, err)
		}

		if err := flags.Set(secret, strings.TrimSpace(string(content))); err != nil {
			return fmt.Errorf("%w: %w", errSetFlagFailed, err)
		}
	}

	return nil
}

// isFilePath determines if a string likely represents a file path.
// It checks for file existence, avoiding false positives from URLs or invalid Windows paths.
func isFilePath(path string) bool {
	firstColon := strings.IndexRune(path, ':')
	if firstColon != 1 && firstColon != -1 {
		// If ':' exists but isn’t the second character, it’s likely not a file path (e.g., URLs).
		return false
	}

	_, err := os.Stat(path)

	return !errors.Is(err, os.ErrNotExist)
}

// ProcessFlagAliases synchronizes flag values based on helper flags and environment settings.
// It adjusts notification, schedule, and logging settings, exiting on invalid configurations.
func ProcessFlagAliases(flags *pflag.FlagSet) {
	porcelain, err := flags.GetString("porcelain")
	if err != nil {
		logrus.Fatalf("Failed to get flag: %v", err)
	}

	if porcelain != "" {
		if porcelain != "v1" {
			logrus.Fatalf("Unknown porcelain version %q. Supported values: \"v1\"", porcelain)
		}

		if err = appendFlagValue(flags, "notification-url", "logger://"); err != nil {
			logrus.Errorf("Failed to set flag: %v", err)
		}

		setFlagIfDefault(flags, "notification-log-stdout", "true")
		setFlagIfDefault(flags, "notification-report", "true")

		tpl := fmt.Sprintf("porcelain.%s.summary-no-log", porcelain)
		setFlagIfDefault(flags, "notification-template", tpl)
	}

	scheduleChanged := flags.Changed("schedule")
	intervalChanged := flags.Changed("interval")
	// Workaround for Viper default swapping issue: check if values differ from defaults.
	if val, _ := flags.GetString("schedule"); val != "" {
		scheduleChanged = true
	}

	if val, _ := flags.GetInt("interval"); val != defaultPollIntervalSeconds {
		intervalChanged = true
	}

	if intervalChanged && scheduleChanged {
		logrus.Fatal("Only schedule or interval can be defined, not both.")
	}

	// Update schedule to match interval or default if needed.
	if intervalChanged || !scheduleChanged {
		interval, _ := flags.GetInt("interval")
		if err := flags.Set("schedule", fmt.Sprintf("@every %ds", interval)); err != nil {
			logrus.Errorf("Failed to set schedule flag: %v", err)
		}
	}

	if flagIsEnabled(flags, "debug") {
		if err := flags.Set("log-level", "debug"); err != nil {
			logrus.Errorf("Failed to set log-level flag: %v", err)
		}
	}

	if flagIsEnabled(flags, "trace") {
		if err := flags.Set("log-level", "trace"); err != nil {
			logrus.Errorf("Failed to set log-level flag: %v", err)
		}
	}
}

// SetupLogging configures the global logger based on log-related flags.
// It sets the log format and level, returning an error for invalid configurations.
func SetupLogging(flags *pflag.FlagSet) error {
	logFormat, err := flags.GetString("log-format")
	if err != nil {
		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	noColor, err := flags.GetBool("no-color")
	if err != nil {
		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	if err := configureLogFormat(logFormat, noColor); err != nil {
		return err
	}

	rawLogLevel, err := flags.GetString("log-level")
	if err != nil {
		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	logLevel, err := logrus.ParseLevel(rawLogLevel)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidLogLevel, err)
	}

	logrus.SetLevel(logLevel)

	return nil
}

// configureLogFormat sets the logrus formatter based on the specified format and color preference.
// It returns an error if the format is invalid.
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
		return fmt.Errorf("%w: %s", errInvalidLogFormat, logFormat)
	}

	return nil
}

// flagIsEnabled checks if a boolean flag is set to true.
// It exits with a fatal error if the flag is not defined.
func flagIsEnabled(flags *pflag.FlagSet, name string) bool {
	value, err := flags.GetBool(name)
	if err != nil {
		logrus.Fatalf("The flag %q is not defined", name)
	}

	return value
}

// appendFlagValue appends values to a slice-type flag.
// It returns an error if the flag is invalid or not a slice.
func appendFlagValue(flags *pflag.FlagSet, name string, values ...string) error {
	flag := flags.Lookup(name)
	if flag == nil {
		return fmt.Errorf("%w: %q", errInvalidFlagName, name)
	}

	if flagValues, ok := flag.Value.(pflag.SliceValue); ok {
		for _, value := range values {
			if err := flagValues.Append(value); err != nil {
				logrus.Errorf("Failed to append value to flag %q: %v", name, err)
			}
		}
	} else {
		return fmt.Errorf("%w: %q", errNotSliceValue, name)
	}

	return nil
}

// setFlagIfDefault sets a flag’s value if it hasn’t been explicitly changed.
// It logs an error if the set operation fails but continues execution.
func setFlagIfDefault(flags *pflag.FlagSet, name string, value string) {
	if flags.Changed(name) {
		return
	}

	if err := flags.Set(name, value); err != nil {
		logrus.Errorf("Failed to set flag: %v", err)
	}
}
