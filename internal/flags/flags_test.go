// Package flags provides tests for Watchtower’s flag and environment variable handling.
package flags

import (
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnvConfig_Defaults verifies that default Docker environment variables are set correctly.
// It ensures the fallback values are applied when no custom flags are provided.
//
//nolint:paralleltest // Omit parallel testing.
func TestEnvConfig_Defaults(t *testing.T) {
	// Unset testing environment variables to isolate defaults.
	_ = os.Unsetenv("DOCKER_TLS_VERIFY")
	_ = os.Unsetenv("DOCKER_HOST")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)

	err := EnvConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "unix:///var/run/docker.sock", os.Getenv("DOCKER_HOST"))
	assert.Equal(t, "", os.Getenv("DOCKER_TLS_VERIFY"))
	assert.Equal(t, DockerAPIMinVersion, os.Getenv("DOCKER_API_VERSION"))
}

// TestEnvConfig_Custom verifies that custom Docker flags override default environment variables.
// It tests setting specific host, TLS, and API version values.
//
//nolint:paralleltest // Omit parallel testing.
func TestEnvConfig_Custom(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)

	err := cmd.ParseFlags([]string{"--host", "some-custom-docker-host", "--tlsverify", "--api-version", "1.99"})
	require.NoError(t, err)

	err = EnvConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "some-custom-docker-host", os.Getenv("DOCKER_HOST"))
	assert.Equal(t, "1", os.Getenv("DOCKER_TLS_VERIFY"))
	assert.Equal(t, "1.99", os.Getenv("DOCKER_API_VERSION"))
}

// TestGetSecretsFromFilesWithString verifies that a string secret flag retains its value.
// It tests direct string input without file substitution.
func TestGetSecretsFromFilesWithString(t *testing.T) {
	value := "supersecretstring"
	t.Setenv("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD", value)

	testGetSecretsFromFiles(t, "notification-email-server-password", value)
}

// TestGetSecretsFromFilesWithFile verifies that a secret flag reads from a file correctly.
// It tests substituting a file path with its contents.
func TestGetSecretsFromFilesWithFile(t *testing.T) {
	value := "megasecretstring"

	// Create a temporary file with the secret.
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)

	_, err = file.WriteString(value)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	t.Setenv("WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD", file.Name())

	testGetSecretsFromFiles(t, "notification-email-server-password", value)
}

// TestGetSliceSecretsFromFiles verifies that a slice secret flag combines file and direct values.
// It tests reading multiple values, including from a file.
//
//nolint:paralleltest // Omit parallel testing.
func TestGetSliceSecretsFromFiles(t *testing.T) {
	values := []string{"entry2", "", "entry3"}

	// Create a temporary file with secret entries.
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)

	for _, value := range values {
		_, err = file.WriteString("\n" + value)
		require.NoError(t, err)
	}

	require.NoError(t, file.Close())

	testGetSecretsFromFiles(t, "notification-url", "[entry1,entry2,entry3]",
		"--notification-url", "entry1",
		"--notification-url", file.Name())
}

// TestHTTPAPIPeriodicPollsFlag verifies the HTTP API periodic polls flag enables correctly.
// It ensures the flag sets the expected boolean value.
//
//nolint:paralleltest // Omit parallel testing.
func TestHTTPAPIPeriodicPollsFlag(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)

	err := cmd.ParseFlags([]string{"--http-api-periodic-polls"})
	require.NoError(t, err)

	periodicPolls, err := cmd.PersistentFlags().GetBool("http-api-periodic-polls")
	require.NoError(t, err)

	assert.True(t, periodicPolls)
}

// TestIsFile verifies the isFilePath function distinguishes files from non-files.
// It tests both URL-like strings and actual file paths.
//
//nolint:paralleltest // Omit parallel testing.
func TestIsFile(t *testing.T) {
	assert.False(t, isFilePath("https://google.com"), "an URL should never be considered a file")
	assert.True(t, isFilePath(os.Args[0]), "the currently running binary path should always be considered a file")
}

// TestProcessFlagAliases verifies that flag aliases are processed correctly.
// It tests porcelain mode, interval, and trace settings.
//
//nolint:paralleltest // Omit parallel testing.
func TestProcessFlagAliases(t *testing.T) {
	logrus.StandardLogger().ExitFunc = func(_ int) { t.FailNow() }
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	require.NoError(t, cmd.ParseFlags([]string{
		"--porcelain", "v1",
		"--interval", "10",
		"--trace",
	}))

	flags := cmd.Flags()
	ProcessFlagAliases(flags)

	urls, _ := flags.GetStringArray("notification-url")
	assert.Contains(t, urls, "logger://")

	logStdout, _ := flags.GetBool("notification-log-stdout")
	assert.True(t, logStdout)

	report, _ := flags.GetBool("notification-report")
	assert.True(t, report)

	template, _ := flags.GetString("notification-template")
	assert.Equal(t, "porcelain.v1.summary-no-log", template)

	sched, _ := flags.GetString("schedule")
	assert.Equal(t, "@every 10s", sched)

	logLevel, _ := flags.GetString("log-level")
	assert.Equal(t, "trace", logLevel)
}

// TestProcessFlagAliasesLogLevelFromEnvironment verifies log level setting from environment.
// It ensures debug mode is enabled via environment variable.
func TestProcessFlagAliasesLogLevelFromEnvironment(t *testing.T) {
	cmd := new(cobra.Command)

	t.Setenv("WATCHTOWER_DEBUG", "true")

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	require.NoError(t, cmd.ParseFlags([]string{}))
	flags := cmd.Flags()
	ProcessFlagAliases(flags)

	logLevel, _ := flags.GetString("log-level")
	assert.Equal(t, "debug", logLevel)
}

// TestLogFormatFlag verifies that log format flags configure the logger correctly.
// It tests various format options and their effects.
//
//nolint:exhaustruct,paralleltest // Intentionally omit fields irrelevant to tests. Omit parallel testing.
func TestLogFormatFlag(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)

	// Test default "Auto" format.
	require.NoError(t, cmd.ParseFlags([]string{}))
	require.NoError(t, SetupLogging(cmd.Flags()))
	assert.IsType(t, &logrus.TextFormatter{}, logrus.StandardLogger().Formatter)

	// Test JSON format.
	require.NoError(t, cmd.ParseFlags([]string{"--log-format", "JSON"}))
	require.NoError(t, SetupLogging(cmd.Flags()))
	assert.IsType(t, &logrus.JSONFormatter{}, logrus.StandardLogger().Formatter)

	// Test Pretty format.
	require.NoError(t, cmd.ParseFlags([]string{"--log-format", "pretty"}))
	require.NoError(t, SetupLogging(cmd.Flags()))
	assert.IsType(t, &logrus.TextFormatter{}, logrus.StandardLogger().Formatter)
	textFormatter, isOk := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	assert.True(t, isOk)
	assert.True(t, textFormatter.ForceColors)
	assert.False(t, textFormatter.FullTimestamp)

	// Test LogFmt format.
	require.NoError(t, cmd.ParseFlags([]string{"--log-format", "logfmt"}))
	require.NoError(t, SetupLogging(cmd.Flags()))

	textFormatter, isOk = logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
	assert.True(t, isOk)
	assert.True(t, textFormatter.DisableColors)
	assert.True(t, textFormatter.FullTimestamp)

	// Test invalid format.
	require.NoError(t, cmd.ParseFlags([]string{"--log-format", "cowsay"}))
	require.Error(t, SetupLogging(cmd.Flags()))
}

// TestLogLevelFlag verifies that an invalid log level flag results in an error.
// It ensures proper validation of log level settings.
//
//nolint:paralleltest // Omit parallel testing.
func TestLogLevelFlag(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)

	require.NoError(t, cmd.ParseFlags([]string{"--log-level", "gossip"}))
	require.Error(t, SetupLogging(cmd.Flags()))
}

// TestProcessFlagAliasesSchedAndInterval verifies that conflicting schedule and interval flags fail.
// It ensures mutual exclusivity is enforced.
//
//nolint:paralleltest // Omit parallel testing.
func TestProcessFlagAliasesSchedAndInterval(t *testing.T) {
	logrus.StandardLogger().ExitFunc = func(_ int) { panic("FATAL") }
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	require.NoError(t, cmd.ParseFlags([]string{"--schedule", "@hourly", "--interval", "10"}))
	flags := cmd.Flags()

	assert.PanicsWithValue(t, "FATAL", func() {
		ProcessFlagAliases(flags)
	})
}

// TestProcessFlagAliasesScheduleFromEnvironment verifies schedule setting from environment.
// It ensures environment variables override defaults correctly.
func TestProcessFlagAliasesScheduleFromEnvironment(t *testing.T) {
	cmd := new(cobra.Command)

	t.Setenv("WATCHTOWER_SCHEDULE", "@hourly")

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	require.NoError(t, cmd.ParseFlags([]string{}))
	flags := cmd.Flags()
	ProcessFlagAliases(flags)

	sched, _ := flags.GetString("schedule")
	assert.Equal(t, "@hourly", sched)
}

// TestProcessFlagAliasesInvalidPorcelaineVersion verifies that an invalid porcelain version fails.
// It ensures version validation triggers a fatal error.
//
//nolint:paralleltest // Omit parallel testing.
func TestProcessFlagAliasesInvalidPorcelaineVersion(t *testing.T) {
	logrus.StandardLogger().ExitFunc = func(_ int) { panic("FATAL") }
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	require.NoError(t, cmd.ParseFlags([]string{"--porcelain", "cowboy"}))
	flags := cmd.Flags()

	assert.PanicsWithValue(t, "FATAL", func() {
		ProcessFlagAliases(flags)
	})
}

// TestFlagsArePresentInDocumentation verifies that all flags are documented.
// It checks documentation files for flag and environment variable mentions.
//
//nolint:paralleltest // Omit parallel testing.
func TestFlagsArePresentInDocumentation(t *testing.T) {
	// Legacy notifications ignored due to soft deprecation.
	ignoredEnvs := map[string]string{
		"WATCHTOWER_NOTIFICATION_SLACK_ICON_EMOJI": "legacy",
		"WATCHTOWER_NOTIFICATION_SLACK_ICON_URL":   "legacy",
	}

	ignoredFlags := map[string]string{
		"notification-gotify-url":       "legacy",
		"notification-slack-icon-emoji": "legacy",
		"notification-slack-icon-url":   "legacy",
	}

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	flags := cmd.PersistentFlags()

	docFiles := []string{
		"../../docs/arguments.md",
		"../../docs/lifecycle-hooks.md",
		"../../docs/notifications.md",
	}
	allDocs := ""

	for _, f := range docFiles {
		bytes, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("Could not load docs file %q: %v", f, err)
		}

		allDocs += string(bytes)
	}

	flags.VisitAll(func(flag *pflag.Flag) {
		if !strings.Contains(allDocs, "--"+flag.Name) {
			if _, found := ignoredFlags[flag.Name]; !found {
				t.Logf("Docs does not mention flag long name %q", flag.Name)
				t.Fail()
			}
		}

		if !strings.Contains(allDocs, "-"+flag.Shorthand) {
			t.Logf("Docs does not mention flag shorthand %q (%q)", flag.Shorthand, flag.Name)
			t.Fail()
		}
	})

	for _, key := range viper.AllKeys() {
		envKey := strings.ToUpper(key)
		if !strings.Contains(allDocs, envKey) {
			if _, found := ignoredEnvs[envKey]; !found {
				t.Logf("Docs does not mention environment variable %q", envKey)
				t.Fail()
			}
		}
	}
}

// testGetSecretsFromFiles is a helper function to test secret retrieval from flags or files.
// It sets up a command, applies arguments, and checks the resulting flag value.
func testGetSecretsFromFiles(t *testing.T, flagName string, expected string, args ...string) {
	t.Helper() // Mark as helper to improve stack trace readability.

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	GetSecretsFromFiles(cmd)
	flag := cmd.PersistentFlags().Lookup(flagName)
	require.NotNil(t, flag)
	value := flag.Value.String()

	assert.Equal(t, expected, value)
}
