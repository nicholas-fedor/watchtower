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

// DockerAPIMinVersion sets the minimum Docker API version supported by Watchtower.
const DockerAPIMinVersion string = "1.24"

// defaultPollIntervalSeconds sets the default polling interval (24 hours).
const defaultPollIntervalSeconds = 86400 // 24 * 60 * 60 seconds

// defaultStopTimeoutSeconds sets the default container stop timeout (30 seconds).
const defaultStopTimeoutSeconds = 30

// defaultEmailServerPort sets the default SMTP port (25).
const defaultEmailServerPort = 25

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
	// errReadFileFailed indicates a failure to read a file’s contents for secrets.
	errReadFileFailed = errors.New("failed to read secret file")
	// errSetFlagFailed indicates a failure to set a flag’s value during configuration.
	errSetFlagFailed = errors.New("failed to set flag value")
	// errInvalidFlagName indicates an invalid flag name was provided for modification.
	errInvalidFlagName = errors.New("invalid flag name provided")
	// errNotSliceValue indicates a flag does not support slice values for appending.
	errNotSliceValue = errors.New("flag does not support slice values")
)

// RegisterDockerFlags adds Docker API client flags to the root command.
//
// Parameters:
//   - rootCmd: Root Cobra command.
func RegisterDockerFlags(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()
	flags.StringP("host", "H", envString("DOCKER_HOST"), "daemon socket to connect to")
	flags.BoolP("tlsverify", "v", envBool("DOCKER_TLS_VERIFY"), "use TLS and verify the remote")
	flags.StringP(
		"api-version",
		"a",
		strings.Trim(envString("DOCKER_API_VERSION"), "\""),
		"api version to use by docker client (omit for autonegotiation)",
	)
}

// RegisterSystemFlags adds Watchtower flow control flags to the root command.
//
// Parameters:
//   - rootCmd: Root Cobra command.
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
		"update-on-start",
		"",
		envBool("WATCHTOWER_UPDATE_ON_START"),
		"Perform an update check on startup, then continue with periodic updates")

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
		"no-self-update",
		"",
		envBool("WATCHTOWER_NO_SELF_UPDATE"),
		"Disable self-update of the Watchtower container")

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
		"http-api-host",
		"",
		envString("WATCHTOWER_HTTP_API_HOST"),
		"Host to bind the HTTP API to (default: empty, binds to all interfaces; allows empty or valid IP address)",
	)

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

	flags.BoolP(
		"disable-memory-swappiness",
		"",
		envBool("WATCHTOWER_DISABLE_MEMORY_SWAPPINESS"),
		"Label used for setting memory swappiness as nil when recreating the container, used for compatibility with podman",
	)

	flags.StringP(
		"cpu-copy-mode",
		"",
		envString("WATCHTOWER_CPU_COPY_MODE"),
		"CPU copy mode for container recreation, used for compatibility with Podman. Options: auto, full, none",
	)

	flags.IntP(
		"lifecycle-uid",
		"",
		envInt("WATCHTOWER_LIFECYCLE_UID"),
		"Default UID to run lifecycle hooks as (can be overridden by container labels)",
	)

	flags.IntP(
		"lifecycle-gid",
		"",
		envInt("WATCHTOWER_LIFECYCLE_GID"),
		"Default GID to run lifecycle hooks as (can be overridden by container labels)",
	)

	flags.Bool(
		"registry-tls-skip",
		envBool("WATCHTOWER_REGISTRY_TLS_SKIP"),
		"Disable TLS verification for registry connections; allows HTTP or insecure TLS registries (use with caution)",
	)
	viper.MustBindEnv("WATCHTOWER_REGISTRY_TLS_SKIP")

	flags.String(
		"registry-tls-min-version",
		envString("WATCHTOWER_REGISTRY_TLS_MIN_VERSION"),
		"Minimum TLS version for registry connections (e.g., TLS1.0, TLS1.1, TLS1.2, TLS1.3); default is TLS1.2",
	)
	viper.MustBindEnv("WATCHTOWER_REGISTRY_TLS_MIN_VERSION")
}

// RegisterNotificationFlags adds notification flags to the root command.
//
// Parameters:
//   - rootCmd: Root Cobra command.
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

	flags.BoolP(
		"notification-split-by-container",
		"",
		envBool("WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER"),
		"Send separate notifications for each updated container instead of grouping them")
}

// RegisterGitFlags adds Git monitoring flags to the root command.
//
// Parameters:
//   - rootCmd: Root Cobra command.
func RegisterGitFlags(rootCmd *cobra.Command) {
	flags := rootCmd.PersistentFlags()

	flags.BoolP(
		"enable-git-monitoring",
		"",
		envBool("WATCHTOWER_GIT_ENABLE"),
		"Enable Git repository monitoring for container updates")

	flags.StringP(
		"git-auth-token",
		"",
		envString("WATCHTOWER_GIT_AUTH_TOKEN"),
		"Authentication token for Git repository access (GitHub/GitLab token)")

	flags.StringP(
		"git-default-branch",
		"",
		envString("WATCHTOWER_GIT_DEFAULT_BRANCH"),
		"Default branch to monitor for Git repositories (default: main)")

	flags.StringP(
		"git-update-policy",
		"",
		envString("WATCHTOWER_GIT_UPDATE_POLICY"),
		"Git update policy: patch, minor, major, or all (default: minor)")
}

// envString fetches a string from an environment variable.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - string: Value or empty if unset.
func envString(key string) string {
	viper.MustBindEnv(key)

	return viper.GetString(key)
}

// envStringSlice fetches a string slice from an environment variable.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - []string: Values or empty slice if unset.
func envStringSlice(key string) []string {
	viper.MustBindEnv(key)

	return viper.GetStringSlice(key)
}

// envInt fetches an integer from an environment variable.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - int: Value or 0 if unset.
func envInt(key string) int {
	viper.MustBindEnv(key)

	return viper.GetInt(key)
}

// envBool fetches a boolean from an environment variable.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - bool: Value or false if unset.
func envBool(key string) bool {
	viper.MustBindEnv(key)

	return viper.GetBool(key)
}

// envDuration fetches a duration from an environment variable.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - time.Duration: Value or 0 if unset.
func envDuration(key string) time.Duration {
	viper.MustBindEnv(key)

	return viper.GetDuration(key)
}

// SetDefaults sets default environment variable values.
//
// It configures fallback values for unset flags.
func SetDefaults() {
	viper.AutomaticEnv()
	viper.SetDefault("DOCKER_HOST", "unix:///var/run/docker.sock")
	viper.SetDefault("WATCHTOWER_POLL_INTERVAL", defaultPollIntervalSeconds)
	viper.SetDefault("WATCHTOWER_TIMEOUT", time.Second*defaultStopTimeoutSeconds)
	viper.SetDefault("WATCHTOWER_HTTP_API_HOST", "")
	viper.SetDefault("WATCHTOWER_HTTP_API_PORT", "8080")
	viper.SetDefault("WATCHTOWER_NOTIFICATIONS", []string{})
	viper.SetDefault("WATCHTOWER_NOTIFICATIONS_LEVEL", "info")
	viper.SetDefault("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT", defaultEmailServerPort)
	viper.SetDefault("WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG", "")
	viper.SetDefault("WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER", "watchtower")
	viper.SetDefault("WATCHTOWER_LOG_LEVEL", "info")
	viper.SetDefault("WATCHTOWER_LOG_FORMAT", "auto")
	viper.SetDefault("WATCHTOWER_DISABLE_MEMORY_SWAPPINESS", false)
	viper.SetDefault("WATCHTOWER_CPU_COPY_MODE", "auto")
	viper.SetDefault("WATCHTOWER_REGISTRY_TLS_SKIP", false)
	viper.SetDefault("WATCHTOWER_REGISTRY_TLS_MIN_VERSION", "TLS1.2")
}

// EnvConfig sets Docker environment variables from flags.
//
// Parameters:
//   - cmd: Cobra command with flags.
//
// Returns:
//   - error: Non-nil if flag retrieval fails, nil on success.
func EnvConfig(cmd *cobra.Command) error {
	flags := cmd.PersistentFlags()

	// Fetch Docker flags.
	host, err := flags.GetString("host")
	if err != nil {
		logrus.WithError(err).WithField("flag", "host").Debug("Failed to get host flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	tls, err := flags.GetBool("tlsverify")
	if err != nil {
		logrus.WithError(err).WithField("flag", "tlsverify").Debug("Failed to get tlsverify flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	version, err := flags.GetString("api-version")
	if err != nil {
		logrus.WithError(err).
			WithField("flag", "api-version").
			Debug("Failed to get api-version flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	// Set environment variables.
	if err := setEnvOptStr("DOCKER_HOST", host); err != nil {
		return err
	}

	if err := setEnvOptBool("DOCKER_TLS_VERIFY", tls); err != nil {
		return err
	}

	if err := setEnvOptStr("DOCKER_API_VERSION", version); err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"host":    host,
		"tls":     tls,
		"version": version,
	}).Debug("Configured Docker environment variables")

	return nil
}

// ReadFlags retrieves key operational flags.
//
// Parameters:
//   - cmd: Cobra command with flags.
//
// Returns:
//   - bool: Cleanup setting.
//   - bool: No-restart setting.
//   - bool: Monitor-only setting.
//   - bool: No-self-update setting.
//   - time.Duration: Stop timeout.
func ReadFlags(cmd *cobra.Command) (bool, bool, bool, bool, time.Duration) {
	flags := cmd.PersistentFlags()

	// Fetch flags, fatal on error.
	cleanup, err := flags.GetBool("cleanup")
	if err != nil {
		logrus.WithField("flag", "cleanup").
			WithError(err).
			Fatal("Failed to get cleanup flag")
	}

	noRestart, err := flags.GetBool("no-restart")
	if err != nil {
		logrus.WithField("flag", "no-restart").
			WithError(err).
			Fatal("Failed to get no-restart flag")
	}

	monitorOnly, err := flags.GetBool("monitor-only")
	if err != nil {
		logrus.WithField("flag", "monitor-only").
			WithError(err).
			Fatal("Failed to get monitor-only flag")
	}

	noSelfUpdate, err := flags.GetBool("no-self-update")
	if err != nil {
		logrus.WithField("flag", "no-self-update").
			WithError(err).
			Fatal("Failed to get no-self-update flag")
	}

	timeout, err := flags.GetDuration("stop-timeout")
	if err != nil {
		logrus.WithField("flag", "stop-timeout").
			WithError(err).
			Fatal("Failed to get stop-timeout flag")
	}

	logrus.WithFields(logrus.Fields{
		"cleanup":        cleanup,
		"no_restart":     noRestart,
		"monitor_only":   monitorOnly,
		"no_self_update": noSelfUpdate,
		"timeout":        timeout,
	}).Debug("Retrieved operational flags")

	return cleanup, noRestart, monitorOnly, noSelfUpdate, timeout
}

// setEnvOptStr sets an environment variable if needed.
//
// Parameters:
//   - env: Environment variable name.
//   - opt: Value to set.
//
// Returns:
//   - error: Non-nil if set fails, nil if skipped or successful.
func setEnvOptStr(env string, opt string) error {
	if opt == "" || opt == os.Getenv(env) {
		return nil
	}

	if err := os.Setenv(env, opt); err != nil {
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

	// Process each secret flag.
	for _, secret := range secrets {
		if err := getSecretFromFile(flags, secret); err != nil {
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
				defer file.Close()

				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					line := scanner.Text()
					if line != "" {
						values = append(values, line)
					}
				}

				if err := scanner.Err(); err != nil {
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

		if err := sliceValue.Replace(values); err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to replace slice value in flag")

			return fmt.Errorf("%w: %w", errReplaceSliceFailed, err)
		}

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

		if err := flags.Set(secret, strings.TrimSpace(string(content))); err != nil {
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
		// If ':' exists but isn’t the second character, it’s likely not a file path (e.g., URLs).
		return false
	}

	_, err := os.Stat(path)

	return !errors.Is(err, os.ErrNotExist)
}

// ProcessFlagAliases syncs flag values based on aliases.
//
// Parameters:
//   - flags: Flag set.
func ProcessFlagAliases(flags *pflag.FlagSet) {
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

		if err := appendFlagValue(flags, "notification-url", "logger://"); err != nil {
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

	if val, _ := flags.GetInt("interval"); val != defaultPollIntervalSeconds {
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
		if err := flags.Set("schedule", scheduleValue); err != nil {
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
		if err := flags.Set("log-level", "debug"); err != nil {
			logrus.WithError(err).Debug("Failed to set debug log level")
		}
	}

	if flagIsEnabled(flags, "trace") {
		if err := flags.Set("log-level", "trace"); err != nil {
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

	noColor, err := flags.GetBool("no-color")
	if err != nil {
		logrus.WithField("flag", "no-color").WithError(err).Debug("Failed to get no-color flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	if err := configureLogFormat(logFormat, noColor); err != nil {
		return err
	}

	// Set log level.
	rawLogLevel, err := flags.GetString("log-level")
	if err != nil {
		logrus.WithField("flag", "log-level").WithError(err).Debug("Failed to get log-level flag")

		return fmt.Errorf("%w: %w", errSetFlagFailed, err)
	}

	logLevel, err := logrus.ParseLevel(rawLogLevel)
	if err != nil {
		logrus.WithError(err).WithField("level", rawLogLevel).Debug("Invalid log level specified")

		return fmt.Errorf("%w: %w", errInvalidLogLevel, err)
	}

	logrus.SetLevel(logLevel)
	logrus.WithFields(logrus.Fields{
		"format":   logFormat,
		"level":    logLevel,
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
			if err := flagValues.Append(value); err != nil {
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

// setFlagIfDefault sets a flag’s default value if unchanged.
//
// Parameters:
//   - flags: Flag set.
//   - name: Flag name.
//   - value: Default value.
func setFlagIfDefault(flags *pflag.FlagSet, name string, value string) {
	if flags.Changed(name) {
		return
	}

	if err := flags.Set(name, value); err != nil {
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
