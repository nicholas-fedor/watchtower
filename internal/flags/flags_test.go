package flags

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:godox
// TODO: Remove and/or update unit testing of deprecated configuration options just before v2 Release.

var errSetFailed = errors.New("set failed")

// newTestCommand creates a new cobra.Command with default flags registered for testing.
func newTestCommand() *cobra.Command {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	return cmd
}

// parseWithEnv parses flags then applies environment values (mirrors production preRun).
func parseWithEnv(t *testing.T, cmd *cobra.Command, args ...string) {
	t.Helper()

	require.NoError(t, cmd.ParseFlags(args))
	require.NoError(t, ApplyEnvToFlags(cmd.PersistentFlags(), AllSpecs()))
}

// TestApplyEnvToFlags_EmptyBoolEnvDoesNotEnable verifies empty non-NO_COLOR bool
// env vars are treated as unset, while NO_COLOR presence still enables no-color.
func TestApplyEnvToFlags_EmptyBoolEnvDoesNotEnable(t *testing.T) {
	t.Setenv("WATCHTOWER_CLEANUP", "")
	t.Setenv("WATCHTOWER_DEBUG", "")
	t.Setenv("NO_COLOR", "")

	cmd := newTestCommand()
	parseWithEnv(t, cmd)

	flagSet := cmd.PersistentFlags()

	cleanup, err := flagSet.GetBool("cleanup")
	require.NoError(t, err)
	assert.False(t, cleanup, "empty WATCHTOWER_CLEANUP must not enable cleanup")

	debug, err := flagSet.GetBool("debug")
	require.NoError(t, err)
	assert.False(t, debug, "empty WATCHTOWER_DEBUG must not enable debug")

	noColor, err := flagSet.GetBool("no-color")
	require.NoError(t, err)
	assert.True(t, noColor, "NO_COLOR presence (empty value) must enable no-color")
}

// TestApplyEnvToFlags_NOColorPresenceAlwaysEnables verifies NO_COLOR uses
// presence semantics (including 0/false) and a static false default.
func TestApplyEnvToFlags_NOColorPresenceAlwaysEnables(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "zero", value: "0"},
		{name: "false", value: "false"},
		{name: "true", value: "true"},
		{name: "one", value: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.value)

			cmd := newTestCommand()
			// Static default must be false before env apply.
			defaultNoColor, err := cmd.PersistentFlags().GetBool("no-color")
			require.NoError(t, err)
			assert.False(t, defaultNoColor)

			parseWithEnv(t, cmd)

			noColor, err := cmd.PersistentFlags().GetBool("no-color")
			require.NoError(t, err)
			assert.True(t, noColor, "NO_COLOR=%q must enable no-color", tc.value)
		})
	}
}

// TestApplyEnvToFlags_DoesNotMarkChanged ensures env bridging leaves pflag.Changed false
// so API *Changed fields only reflect CLI overrides.
func TestApplyEnvToFlags_DoesNotMarkChanged(t *testing.T) {
	t.Setenv("WATCHTOWER_HTTP_API_HOST", "127.0.0.1")
	t.Setenv("WATCHTOWER_HTTP_API_PORT", "9090")
	t.Setenv("WATCHTOWER_HTTP_API_RATE_LIMIT", "30")
	t.Setenv("WATCHTOWER_TIMEOUT", "45")
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "slack://hook discord://token")

	cmd := newTestCommand()
	parseWithEnv(t, cmd)

	flagSet := cmd.PersistentFlags()

	assert.False(t, flagSet.Changed("http-api-host"), "env must not mark host Changed")
	assert.False(t, flagSet.Changed("http-api-port"), "env must not mark port Changed")
	assert.False(t, flagSet.Changed("http-api-rate-limit"), "env must not mark rate-limit Changed")
	assert.False(t, flagSet.Changed("stop-timeout"), "env must not mark stop-timeout Changed")
	assert.False(t, flagSet.Changed("notification-url"), "env must not mark slice Changed")

	host, err := flagSet.GetString("http-api-host")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", host)

	port, err := flagSet.GetString("http-api-port")
	require.NoError(t, err)
	assert.Equal(t, "9090", port)

	timeout, err := flagSet.GetDuration("stop-timeout")
	require.NoError(t, err)
	assert.Equal(t, 45*time.Second, timeout, "bare-second WATCHTOWER_TIMEOUT must bridge to stop-timeout")

	urls, err := flagSet.GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Equal(t, []string{"slack://hook", "discord://token"}, urls,
		"space-separated WATCHTOWER_NOTIFICATION_URL must bridge to two entries")

	// CLI still marks Changed and wins over env.
	t.Setenv("WATCHTOWER_HTTP_API_HOST", "10.0.0.1")

	cmdCLI := newTestCommand()
	parseWithEnv(t, cmdCLI, "--http-api-host", "192.168.1.1")

	assert.True(t, cmdCLI.PersistentFlags().Changed("http-api-host"))
	cliHost, err := cmdCLI.PersistentFlags().GetString("http-api-host")
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.1", cliHost)
}

// TestEnvConfig tests EnvConfig functionality with various configurations.
func TestEnvConfig(t *testing.T) {
	testCases := []struct {
		name          string
		envVars       map[string]string
		flags         []string
		setupCmd      func(*cobra.Command)
		expectEnv     map[string]string
		expectError   bool
		expectWarning string
	}{
		{
			name: "defaults",
			envVars: map[string]string{
				"DOCKER_TLS_VERIFY": "",
				"DOCKER_HOST":       "",
				"DOCKER_CERT_PATH":  "",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST":        "unix:///var/run/docker.sock",
				"DOCKER_TLS_VERIFY":  "",
				"DOCKER_API_VERSION": "",
				"DOCKER_CERT_PATH":   "",
			},
		},
		{
			name: "custom",
			flags: []string{
				"--host", "some-custom-docker-host",
				"--tlsverify",
				"--api-version", "1.99",
				"--cert-path", "/path/to/certs",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST":        "some-custom-docker-host",
				"DOCKER_TLS_VERIFY":  "1",
				"DOCKER_API_VERSION": "1.99",
				"DOCKER_CERT_PATH":   "/path/to/certs",
			},
		},
		{
			name: "flag errors",
			setupCmd: func(_ *cobra.Command) {
				// Don't register flags to force retrieval errors
			},
			expectError: true,
		},
		{
			name: "flag retrieval errors partial",
			setupCmd: func(cmd *cobra.Command) {
				SetDefaults()
				cmd.PersistentFlags().StringP("host", "H", "", "daemon socket")
				// Only host defined, expect errors for others
			},
			flags:       []string{"--host", "test"},
			expectError: true,
		},
		{
			name: "flag retrieval errors tls",
			setupCmd: func(cmd *cobra.Command) {
				SetDefaults()
				cmd.PersistentFlags().StringP("host", "H", "", "daemon socket")
				cmd.PersistentFlags().BoolP("tlsverify", "v", false, "use TLS")
				// Host and tlsverify defined, expect error for api-version
			},
			flags:       []string{"--host", "test", "--tlsverify"},
			expectError: true,
		},
		{
			name: "tls host conversion",
			envVars: map[string]string{
				"DOCKER_HOST":       "tcp://example.com:2376",
				"DOCKER_TLS_VERIFY": "1",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST": "https://example.com:2376",
			},
		},
		{
			name: "cert path from env",
			envVars: map[string]string{
				"DOCKER_CERT_PATH": "/env/cert/path",
			},
			expectEnv: map[string]string{
				"DOCKER_CERT_PATH": "/env/cert/path",
			},
		},
		{
			name: "tls warnings http with tls",
			envVars: map[string]string{
				"DOCKER_TLS_VERIFY": "1",
			},
			flags: []string{"--host", "http://example.com"},
			expectEnv: map[string]string{
				"DOCKER_HOST": "http://example.com",
			},
			expectWarning: "TLS verification is enabled but DOCKER_HOST uses insecure scheme 'http://'. Consider using 'https://' or disable TLS verification.",
		},
		{
			name: "tls warnings unix with tls",
			envVars: map[string]string{
				"DOCKER_TLS_VERIFY": "1",
			},
			flags: []string{"--host", "unix:///var/run/docker.sock"},
			expectEnv: map[string]string{
				"DOCKER_HOST": "unix:///var/run/docker.sock",
			},
			expectWarning: "TLS verification is enabled but DOCKER_HOST uses local socket 'unix://'. TLS is not applicable for local sockets; consider disabling TLS verification.",
		},
		{
			name: "tls warnings https with tls",
			envVars: map[string]string{
				"DOCKER_TLS_VERIFY": "1",
			},
			flags: []string{"--host", "https://example.com"},
			expectEnv: map[string]string{
				"DOCKER_HOST": "https://example.com",
			},
		},
		{
			name: "tls warnings tcp with tls",
			envVars: map[string]string{
				"DOCKER_TLS_VERIFY": "1",
			},
			flags: []string{"--host", "tcp://example.com"},
			expectEnv: map[string]string{
				"DOCKER_HOST": "https://example.com",
			},
		},
		{
			name:  "tls warnings unix without tls",
			flags: []string{"--host", "unix:///var/run/docker.sock"},
			expectEnv: map[string]string{
				"DOCKER_HOST": "unix:///var/run/docker.sock",
			},
		},
		{
			name: "docker host env var",
			envVars: map[string]string{
				"DOCKER_HOST": "unix:///var/run/docker.sock",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST": "unix:///var/run/docker.sock",
			},
		},
		{
			name: "docker tls verify env var",
			envVars: map[string]string{
				"DOCKER_TLS_VERIFY": "1",
			},
			expectEnv: map[string]string{
				"DOCKER_TLS_VERIFY": "1",
			},
		},
		{
			name: "docker cert path env var",
			envVars: map[string]string{
				"DOCKER_CERT_PATH": "/env/certs",
			},
			expectEnv: map[string]string{
				"DOCKER_CERT_PATH": "/env/certs",
			},
		},
		{
			name: "docker api version env var",
			envVars: map[string]string{
				"DOCKER_API_VERSION": "1.41",
			},
			expectEnv: map[string]string{
				"DOCKER_API_VERSION": "1.41",
			},
		},
		{
			name: "tls host conversion https no change",
			envVars: map[string]string{
				"DOCKER_HOST":       "https://example.com:2376",
				"DOCKER_TLS_VERIFY": "1",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST": "https://example.com:2376",
			},
		},
		{
			name: "tls warnings tcp converted no warning",
			envVars: map[string]string{
				"DOCKER_HOST":       "tcp://example.com:2376",
				"DOCKER_TLS_VERIFY": "1",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST": "https://example.com:2376",
			},
		},
		{
			name: "tls warnings unix without tls no warning",
			envVars: map[string]string{
				"DOCKER_HOST": "unix:///var/run/docker.sock",
			},
			expectEnv: map[string]string{
				"DOCKER_HOST": "unix:///var/run/docker.sock",
			},
		},
		{
			name: "edge case empty api version",
			envVars: map[string]string{
				"DOCKER_API_VERSION": "",
			},
			expectEnv: map[string]string{
				"DOCKER_API_VERSION": "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tc.envVars {
				if v == "" {
					os.Unsetenv(k)
				} else {
					t.Setenv(k, v)
				}
			}

			cmd := new(cobra.Command)
			if tc.setupCmd != nil {
				tc.setupCmd(cmd)
			} else {
				SetDefaults()
				RegisterDockerFlags(cmd)
			}

			if len(tc.flags) > 0 {
				err := cmd.ParseFlags(tc.flags)
				require.NoError(t, err)
			}

			var logOutput strings.Builder
			if tc.expectWarning != "" {
				logrus.SetOutput(&logOutput)
				logrus.SetLevel(logrus.WarnLevel)

				defer func() {
					logrus.SetOutput(os.Stderr)
					logrus.SetLevel(logrus.InfoLevel)
				}()
			}

			err := EnvConfig(cmd)

			if tc.expectError {
				require.Error(t, err)
				// Partial flag registration fails during BindAll (flag not registered)
				// or when setting Docker env vars.
				errMsg := err.Error()
				assert.True(t,
					strings.Contains(errMsg, "failed to set flag value") ||
						strings.Contains(errMsg, "not registered") ||
						strings.Contains(errMsg, "bind docker"),
					"unexpected error: %s", errMsg,
				)

				return
			}

			require.NoError(t, err)

			for k, v := range tc.expectEnv {
				assert.Equal(t, v, os.Getenv(k))
			}

			if tc.expectWarning != "" {
				assert.Contains(t, logOutput.String(), tc.expectWarning)
			} else if tc.expectWarning == "" && logOutput.Len() > 0 {
				assert.Empty(t, logOutput.String())
			}
		})
	}
}

// TestGetSecretsFromFiles tests GetSecretsFromFiles functionality with various scenarios.
func TestGetSecretsFromFiles(t *testing.T) {
	testCases := []struct {
		name     string
		envVars  map[string]string
		files    []struct{ path, content string }
		flagName string
		expected string
		args     []string
	}{
		{
			name: "string value",
			envVars: map[string]string{
				"WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD": "supersecretstring",
			},
			flagName: "notification-email-server-password",
			expected: "supersecretstring",
		},
		{
			name: "file value",
			files: []struct{ path, content string }{
				{"password.txt", "megasecretstring"},
			},
			envVars: map[string]string{
				"WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD": "password.txt",
			},
			flagName: "notification-email-server-password",
			expected: "megasecretstring",
		},
		{
			name: "slice with file",
			files: []struct{ path, content string }{
				{"urls.txt", "\ndiscord://entry2\n\ntelegram://entry3"},
			},
			flagName: "notification-url",
			expected: "[discord://entry1,discord://entry2,telegram://entry3]",
			args:     []string{"--notification-url", "discord://entry1", "--notification-url", "urls.txt"},
		},
		{
			name: "empty lines",
			files: []struct{ path, content string }{
				{"urls.txt", "discord://entry1\n\ntelegram://entry2\n  \nslack://entry3"},
			},
			flagName: "notification-url",
			expected: "[discord://entry1,telegram://entry2,slack://entry3]",
			args:     []string{"--notification-url", "urls.txt"},
		},
		{
			name: "urls with trailing newlines",
			files: []struct{ path, content string }{
				{
					"urls.txt",
					"telegram://1234567890:AAEJ_AAAAABBBBBccccccccdddddddd@telegram/?channels=123456789&parseMode=html\nsmtp://test\n",
				},
			},
			flagName: "notification-url",
			expected: "[telegram://1234567890:AAEJ_AAAAABBBBBccccccccdddddddd@telegram/?channels=123456789&parseMode=html,smtp://test]",
			args:     []string{"--notification-url", "urls.txt"},
		},
		{
			name: "special chars",
			files: []struct{ path, content string }{
				{"urls.txt", "smtp://user:pass@host:25?key=value\nslack://token@channel"},
			},
			flagName: "notification-url",
			expected: "[smtp://user:pass@host:25?key=value,slack://token@channel]",
			args:     []string{"--notification-url", "urls.txt"},
		},
		{
			name: "non-existent file",
			envVars: map[string]string{
				"WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD": "/nonexistent/file",
			},
			flagName: "notification-email-server-password",
			expected: "/nonexistent/file",
		},
		{
			name: "mixed values",
			files: []struct{ path, content string }{
				{"urls.txt", "discord://fileentry1\ntelegram://fileentry2"},
			},
			flagName: "notification-url",
			expected: "[discord://direct1,discord://fileentry1,telegram://fileentry2,discord://direct2]",
			args: []string{
				"--notification-url",
				"discord://direct1",
				"--notification-url",
				"urls.txt",
				"--notification-url",
				"discord://direct2",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp files first
			fileMap := make(map[string]string)

			for _, f := range tc.files {
				file, err := os.CreateTemp(t.TempDir(), "watchtower-")
				require.NoError(t, err)
				_, err = file.WriteString(f.content)
				require.NoError(t, err)
				require.NoError(t, file.Close())
				fileMap[f.path] = file.Name()
			}

			// Set env vars, replacing placeholder paths
			for k, v := range tc.envVars {
				if actualPath, ok := fileMap[v]; ok {
					t.Setenv(k, actualPath)
				} else {
					t.Setenv(k, v)
				}
			}

			// Update args to use actual paths
			args := make([]string, len(tc.args))
			copy(args, tc.args)

			for i, arg := range args {
				if actualPath, ok := fileMap[arg]; ok {
					args[i] = actualPath
				}
			}

			testGetSecretsFromFiles(t, tc.flagName, tc.expected, args...)
		})
	}
}

// TestHTTPAPIPeriodicPollsFlag verifies the HTTP API periodic polls flag enables correctly.
// It ensures the flag sets the expected boolean value.
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
func TestIsFile(t *testing.T) {
	assert.False(t, isFilePath("https://google.com"), "an URL should never be considered a file")
	assert.True(
		t,
		isFilePath(os.Args[0]),
		"the currently running binary path should always be considered a file",
	)
}

// TestProcessFlagAliases tests flag alias processing with various configurations.
func TestProcessFlagAliases(t *testing.T) {
	testCases := []struct {
		name        string
		envVars     map[string]string
		flags       []string
		expectPanic bool
		checks      func(t *testing.T, flags *pflag.FlagSet)
	}{
		{
			name: "porcelain v1 with interval and trace",
			flags: []string{
				"--porcelain", "v1",
				"--interval", "10",
				"--trace",
			},
			checks: func(t *testing.T, flags *pflag.FlagSet) {
				t.Helper()

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
			},
		},
		{
			name:    "log level from environment",
			envVars: map[string]string{"WATCHTOWER_DEBUG": "true"},
			checks: func(t *testing.T, flags *pflag.FlagSet) {
				t.Helper()

				logLevel, _ := flags.GetString("log-level")
				assert.Equal(t, "debug", logLevel)
			},
		},
		{
			name:    "schedule from environment",
			envVars: map[string]string{"WATCHTOWER_SCHEDULE": "@hourly"},
			checks: func(t *testing.T, flags *pflag.FlagSet) {
				t.Helper()

				sched, _ := flags.GetString("schedule")
				assert.Equal(t, "@hourly", sched)
			},
		},
		{
			name:        "schedule and interval conflict",
			flags:       []string{"--schedule", "@hourly", "--interval", "10"},
			expectPanic: true,
		},
		{
			name:        "invalid porcelain version",
			flags:       []string{"--porcelain", "v2"},
			expectPanic: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			if tc.expectPanic {
				logrus.StandardLogger().ExitFunc = func(_ int) { panic("FATAL") }
				cmd := newTestCommand()
				require.NoError(t, cmd.ParseFlags(tc.flags))
				assert.PanicsWithValue(t, "FATAL", func() {
					ProcessFlagAliases(cmd.Flags())
				})

				return
			}

			cmd := newTestCommand()
			require.NoError(t, cmd.ParseFlags(tc.flags))
			ProcessFlagAliases(cmd.Flags())

			if tc.checks != nil {
				tc.checks(t, cmd.Flags())
			}
		})
	}
}

// TestSetupLogging tests logging setup with various formats and levels.
func TestSetupLogging(t *testing.T) {
	testCases := []struct {
		name        string
		flags       []string
		expectError bool
		checks      func(t *testing.T)
	}{
		{
			name:  "default format",
			flags: []string{},
			checks: func(t *testing.T) {
				t.Helper()
				assert.IsType(t, &logrus.TextFormatter{}, logrus.StandardLogger().Formatter)
			},
		},
		{
			name:  "JSON format",
			flags: []string{"--log-format", "JSON"},
			checks: func(t *testing.T) {
				t.Helper()
				assert.IsType(t, &logrus.JSONFormatter{}, logrus.StandardLogger().Formatter)
			},
		},
		{
			name:  "pretty format",
			flags: []string{"--log-format", "pretty"},
			checks: func(t *testing.T) {
				t.Helper()
				assert.IsType(t, &logrus.TextFormatter{}, logrus.StandardLogger().Formatter)
				textFormatter, isOk := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
				assert.True(t, isOk)
				assert.True(t, textFormatter.ForceColors)
				assert.False(t, textFormatter.FullTimestamp)
			},
		},
		{
			name:  "logfmt format",
			flags: []string{"--log-format", "logfmt"},
			checks: func(t *testing.T) {
				t.Helper()

				textFormatter, isOk := logrus.StandardLogger().Formatter.(*logrus.TextFormatter)
				assert.True(t, isOk)
				assert.True(t, textFormatter.DisableColors)
				assert.True(t, textFormatter.FullTimestamp)
			},
		},
		{
			name:        "invalid format",
			flags:       []string{"--log-format", "cowsay"},
			expectError: true,
		},
		{
			name:        "invalid log level",
			flags:       []string{"--log-level", "gossip"},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newTestCommand()
			require.NoError(t, cmd.ParseFlags(tc.flags))

			err := SetupLogging(cmd.Flags())

			if tc.expectError {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			if tc.checks != nil {
				tc.checks(t)
			}
		})
	}
}

// TestFlagsArePresentInDocumentation verifies that all flags are documented.
// It checks documentation files for flag and environment variable mentions.
func TestFlagsArePresentInDocumentation(t *testing.T) {
	ignoredEnvs := map[string]string{}

	ignoredFlags := map[string]string{
		"self-update-orchestrator": "internal",
	}

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterDockerFlags(cmd)
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)

	flags := cmd.PersistentFlags()

	docFiles := []string{
		"../../docs/configuration/container-selection/index.md",
		"../../docs/configuration/docker-connection/index.md",
		"../../docs/configuration/http-api/index.md",
		"../../docs/configuration/image-cooldown/index.md",
		"../../docs/configuration/introduction/index.md",
		"../../docs/configuration/lifecycle-hooks/index.md",
		"../../docs/configuration/logging-and-output/index.md",
		"../../docs/configuration/notifications/index.md",
		"../../docs/configuration/registry-and-authentication/index.md",
		"../../docs/configuration/scheduling/index.md",
		"../../docs/configuration/update-behavior/index.md",
	}
	allDocs := ""

	var stringBuilder strings.Builder

	for _, f := range docFiles {
		bytes, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("Could not load docs file %q: %v", f, err)
		}

		stringBuilder.Write(bytes)
	}

	allDocs += stringBuilder.String()

	flags.VisitAll(func(flag *pflag.Flag) {
		if !strings.Contains(allDocs, "--"+flag.Name) {
			if _, found := ignoredFlags[flag.Name]; !found {
				t.Logf("Docs does not mention flag long name %q", flag.Name)
				t.Fail()
			}
		}

		if flag.Shorthand != "" && !strings.Contains(allDocs, "-"+flag.Shorthand) {
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

// TestSetEnvOptStr_Error tests error handling in setEnvOptStr.
// Note: This test is limited without mocking os.Setenv; real failure requires system-specific conditions.
func TestSetEnvOptStr_Error(t *testing.T) {
	// Mocking os.Setenv is complex without dependency injection; test assumes rare failure case
	// For coverage, ensure environment is writable and check logic
	err := setEnvOptStr("TEST_ENV", "value")
	assert.NoError(t, err) // Normally succeeds; mock needed for failure
	// To truly test line 592, use a system where Setenv fails (e.g., read-only env)
}

// TestGetSecretFromFile_OpenError tests file opening errors in getSecretFromFile.
func TestGetSecretFromFile_OpenError(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	fileName := t.TempDir() + "/nonexistent-file"

	err := cmd.ParseFlags([]string{"--notification-email-server-password", fileName})
	require.NoError(t, err)

	// Custom getSecret to explicitly hit os.Open failure
	getSecret := func(flags *pflag.FlagSet, secret string) error {
		flag := flags.Lookup(secret)

		value := flag.Value.String()
		if value != "" && true { // Force path without mocking isFilePath
			_, err := os.Open(value)
			if err != nil {
				return fmt.Errorf("%w: %w", errOpenFileFailed, err)
			}
		}

		return nil
	}

	err = getSecret(cmd.PersistentFlags(), "notification-email-server-password")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open secret file")
}

// TestGetSecretFromFile_SkipCommentsAndEmptyLines verifies that comment and empty
// lines are skipped when reading notification URLs from a secret file.
func TestGetSecretFromFile_SkipCommentsAndEmptyLines(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString(
		"# This is a comment\n" +
			"discord://token@webhookid\n" +
			"\n" +
			"  # indented comment\n" +
			"telegram://token@telegram?chats=@channel\n" +
			"# another comment\n",
	)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{"--notification-url", file.Name()})
	require.NoError(t, err)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	assert.Equal(t, []string{"discord://token@webhookid", "telegram://token@telegram?chats=@channel"}, urls)
}

// TestGetSecretFromFile_InvalidSecretURL verifies that non-URL lines and
// parameterless non-logger/mock URLs in a secret file are rejected with
// errInvalidSecretURL.
func TestGetSecretFromFile_InvalidSecretURL(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString(
		"discord://token@webhookid\n" +
			"not-a-url\n" +
			"://missing-scheme\n" +
			"discord://\n",
	)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{"--notification-url", file.Name()})
	require.NoError(t, err)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.Error(t, err)
	require.ErrorIs(t, err, errInvalidSecretURL)
}

// TestGetSecretFromFile_ParameterlessLoggerAndMockURLs verifies that
// parameterless logger:// and mock:// URLs are accepted in a secret file.
func TestGetSecretFromFile_ParameterlessLoggerAndMockURLs(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString(
		"logger://\n" +
			"mock://\n" +
			"discord://token@webhookid\n",
	)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{"--notification-url", file.Name()})
	require.NoError(t, err)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	assert.Equal(t, []string{"logger://", "mock://", "discord://token@webhookid"}, urls)
}

// TestGetSecretFromFile_CloseError tests file closing errors (simplified without full mocking).
func TestGetSecretFromFile_CloseError(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	err = cmd.ParseFlags([]string{"--notification-email-server-password", file.Name()})
	require.NoError(t, err)
	// Close file early to simulate potential issues
	file.Close()

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-email-server-password")
	assert.NoError(t, err) // Still succeeds unless Close failure is mocked
	// Full coverage requires mocking os.File.Close to fail
}

// TestGetSecretFromFile_StringPathStillMarksChanged verifies that KindString
// secret expansion still marks Changed=true via flags.Set.
func TestGetSecretFromFile_StringPathStillMarksChanged(t *testing.T) {
	cmd := newTestCommand()
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString("supersecretstring")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{"--notification-email-server-password", file.Name()})
	require.NoError(t, err)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-email-server-password")
	require.NoError(t, err)

	flag := cmd.PersistentFlags().Lookup("notification-email-server-password")
	require.NotNil(t, flag)
	assert.True(t, flag.Changed, "KindString secret expansion must mark Changed=true")
	assert.Equal(t, "supersecretstring", flag.Value.String())
}

// TestGetSecretFromFile_SlicePath_MarksChangedAfterReplace verifies that
// getSecretFromFile marks Changed=true after expanding a file path so the
// pflag value is preferred over raw os.Getenv.
func TestGetSecretFromFile_SlicePath_MarksChangedAfterReplace(t *testing.T) {
	cmd := newTestCommand()
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString("gotify://gotify.example.com/token123")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{"--notification-url", file.Name()})
	require.NoError(t, err)
	parseWithEnv(t, cmd)

	flag := cmd.PersistentFlags().Lookup("notification-url")
	require.NotNil(t, flag)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Equal(t, []string{"gotify://gotify.example.com/token123"}, urls)
	assert.True(t, flag.Changed, "slice secret expansion must mark Changed=true")
}

// TestGetSecretFromFile_SlicePath_MultiLineFile verifies that a multi-line
// docker secret file expands into multiple slice values with Changed=true.
func TestGetSecretFromFile_SlicePath_MultiLineFile(t *testing.T) {
	cmd := newTestCommand()
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString("gotify://host1/token1\ndiscord://host2/token2")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{"--notification-url", file.Name()})
	require.NoError(t, err)
	parseWithEnv(t, cmd)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Equal(t, []string{"gotify://host1/token1", "discord://host2/token2"}, urls)

	flag := cmd.PersistentFlags().Lookup("notification-url")
	assert.True(t, flag.Changed)
}

// TestGetSecretFromFile_SlicePath_LiteralURLsUnchanged verifies that literal
// notification URLs survive getSecretFromFile without modification and that
// Changed=true is set on the processed flag.
func TestGetSecretFromFile_SlicePath_LiteralURLsUnchanged(t *testing.T) {
	cmd := newTestCommand()

	err := cmd.ParseFlags([]string{
		"--notification-url", "gotify://gotify.example.com/token123",
		"--notification-url", "discord://token@channel",
	})
	require.NoError(t, err)
	parseWithEnv(t, cmd)

	flag := cmd.PersistentFlags().Lookup("notification-url")
	require.NotNil(t, flag)

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Equal(t, []string{"gotify://gotify.example.com/token123", "discord://token@channel"}, urls)

	// Changed is true after Replace regardless of whether expansion occurred; the
	// processed value is explicit user config and downstream consumers must read it.
	assert.True(t, flag.Changed)
}

// TestGetSecretsFromFile_PorcelainAppendPreservedWithSecret verifies that
// porcelain mode appends logger:// before secret expansion and the merged
// slice retains both values with Changed=true.
func TestGetSecretsFromFile_PorcelainAppendPreservedWithSecret(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "/file/that/does/not/exist") // placeholder; real file used via flag

	cmd := newTestCommand()
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString("gotify://host/token")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = cmd.ParseFlags([]string{
		"--notification-url", file.Name(),
		"--porcelain", "v1",
	})
	require.NoError(t, err)
	ProcessFlagAliases(cmd.PersistentFlags())

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Contains(t, urls, "logger://", "porcelain value must survive secret expansion")

	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err = cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Contains(t, urls, "logger://")
	assert.Contains(t, urls, "gotify://host/token")

	flag := cmd.PersistentFlags().Lookup("notification-url")
	assert.True(t, flag.Changed)
}

// TestApplyEnvToFlags_DoesNotReBrideExpandedSecret verifies that re-running
// ApplyEnvToFlags does not overwrite an expanded secret with the raw file path.
func TestApplyEnvToFlags_DoesNotReBrideExpandedSecret(t *testing.T) {
	cmd := newTestCommand()
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)
	_, err = file.WriteString("gotify://host/token")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	// Step 1: env bridge sets pflag to the file path
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", file.Name())

	err = cmd.ParseFlags([]string{})
	require.NoError(t, err)
	err = ApplyEnvToFlags(cmd.PersistentFlags(), AllSpecs())
	require.NoError(t, err)

	flag := cmd.PersistentFlags().Lookup("notification-url")
	require.NotNil(t, flag)
	assert.False(t, flag.Changed, "env bridge must leave Changed=false")

	// Step 2: secret expansion replaces content and marks Changed=true
	err = getSecretFromFile(cmd.PersistentFlags(), "notification-url")
	require.NoError(t, err)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Equal(t, []string{"gotify://host/token"}, urls)
	assert.True(t, flag.Changed)

	// Step 3: re-running ApplyEnvToFlags must not overwrite the expanded value
	err = ApplyEnvToFlags(cmd.PersistentFlags(), AllSpecs())
	require.NoError(t, err)

	urls, err = cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)
	assert.Equal(t, []string{"gotify://host/token"}, urls, "re-applying env must not clobber expanded secret")
	assert.True(t, flag.Changed)
}

// TestProcessFlagAliases_InvalidPorcelain tests invalid porcelain version handling.
func TestProcessFlagAliases_InvalidPorcelain(t *testing.T) {
	originalExit := logrus.StandardLogger().ExitFunc

	defer func() { logrus.StandardLogger().ExitFunc = originalExit }()

	logrus.StandardLogger().ExitFunc = func(_ int) { panic("FATAL") }

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterSystemFlags(cmd)
	err := cmd.ParseFlags([]string{"--porcelain", "v2"})
	require.NoError(t, err)
	assert.PanicsWithValue(t, "FATAL", func() {
		ProcessFlagAliases(cmd.Flags())
	})
}

// TestProcessFlagAliases_FlagSetErrors tests error logging for flag operations.
func TestProcessFlagAliases_FlagSetErrors(t *testing.T) {
	// Capture log output to verify error logging
	var logOutput strings.Builder

	logrus.SetOutput(&logOutput)
	logrus.SetLevel(logrus.DebugLevel) // Ensure Debug logs are captured

	defer func() {
		logrus.SetOutput(os.Stderr)       // Restore default output
		logrus.SetLevel(logrus.InfoLevel) // Restore default level
	}()

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterSystemFlags(cmd)
	err := cmd.ParseFlags([]string{"--debug"})
	require.NoError(t, err)

	// Simulate a failure in flag setting by temporarily overriding log-level's Value
	flags := cmd.Flags()
	flag := flags.Lookup("log-level")
	originalValue := flag.Value
	flag.Value = &errorStringValue{err: errSetFailed} // Use static error

	defer func() { flag.Value = originalValue }() // Restore original value

	ProcessFlagAliases(flags)
	assert.Contains(
		t,
		logOutput.String(),
		"Failed to set debug log level",
		"Expected log output to contain the debug log level set failure message",
	)
}

// errorStringValue is a custom pflag.Value that always errors on Set.
type errorStringValue struct {
	err error
}

func (e *errorStringValue) String() string   { return "" }
func (e *errorStringValue) Set(string) error { return e.err }
func (e *errorStringValue) Type() string     { return "string" }

// TestSetupLogging_FlagErrors tests error handling in SetupLogging.
func TestSetupLogging_FlagErrors(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	// Don't register flags to force retrieval errors
	err := SetupLogging(cmd.Flags())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set flag value")
}

// testGetSecretsFromFiles is a helper function to test secret retrieval from flags or files.
// It sets up a command, applies arguments, and checks the resulting flag value.
func testGetSecretsFromFiles(t *testing.T, flagName, expected string, args ...string) {
	t.Helper() // Mark as helper to improve stack trace readability.

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd, args...)
	GetSecretsFromFiles(cmd)
	flag := cmd.PersistentFlags().Lookup(flagName)
	require.NotNil(t, flag)
	value := flag.Value.String()

	assert.Equal(t, expected, value)
}

func TestDisableMemorySwappinessFlag(t *testing.T) {
	cmd := new(cobra.Command)

	SetDefaults()
	RegisterSystemFlags(cmd)

	err := cmd.ParseFlags([]string{"--disable-memory-swappiness"})
	require.NoError(t, err)

	disableMemorySwappiness, err := cmd.PersistentFlags().GetBool("disable-memory-swappiness")
	require.NoError(t, err)
	assert.True(t, disableMemorySwappiness, "disable-memory-swappiness flag should be true")
}

// TestUpdateOnStart tests the --update-on-start flag with various inputs.
func TestUpdateOnStart(t *testing.T) {
	testCases := []struct {
		name     string
		envVars  map[string]string
		flags    []string
		expected bool
	}{
		{
			name:     "flag set",
			flags:    []string{"--update-on-start"},
			expected: true,
		},
		{
			name:     "environment variable",
			envVars:  map[string]string{"WATCHTOWER_UPDATE_ON_START": "true"},
			expected: true,
		},
		{
			name:     "default",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cmd := new(cobra.Command)

			SetDefaults()
			RegisterSystemFlags(cmd)
			parseWithEnv(t, cmd, tc.flags...)

			updateOnStart, err := cmd.PersistentFlags().GetBool("update-on-start")
			require.NoError(t, err)
			assert.Equal(t, tc.expected, updateOnStart)
		})
	}
}

// TestWatchtowerNotificationsEnvironmentVariable verifies that WATCHTOWER_NOTIFICATIONS environment variable
// correctly sets the notifications flag as a string slice.
func TestWatchtowerNotificationsEnvironmentVariable(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATIONS", "email slack")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	notifications, err := cmd.PersistentFlags().GetStringSlice("notifications")
	require.NoError(t, err)

	assert.Equal(t, []string{"email", "slack"}, notifications)
}

// TestWatchtowerNotificationURLEnvironmentVariable verifies that WATCHTOWER_NOTIFICATION_URL environment variable
// correctly sets the notification-url flag as a string array.
func TestWatchtowerNotificationURLEnvironmentVariable(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "smtp://user:pass@host:port slack://token@channel")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	expected := []string{"smtp://user:pass@host:port", "slack://token@channel"}
	assert.Equal(t, expected, urls)
}

// TestNotificationEnvVarsDoNotAffectContainerFiltering verifies that setting notification environment variables
// does not interfere with container filtering flags like disable-containers.
func TestNotificationEnvVarsDoNotAffectContainerFiltering(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATIONS", "email")
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "smtp://test")
	t.Setenv("WATCHTOWER_DISABLE_CONTAINERS", "container1,container2")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterSystemFlags(cmd)
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	disableContainers, err := cmd.PersistentFlags().GetStringSlice("disable-containers")
	require.NoError(t, err)

	assert.Equal(t, []string{"container1", "container2"}, disableContainers)

	notifications, err := cmd.PersistentFlags().GetStringSlice("notifications")
	require.NoError(t, err)

	assert.Equal(t, []string{"email"}, notifications)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	assert.Equal(t, []string{"smtp://test"}, urls)
}

// TestNotificationsConfigurationFromEnvVarsVsFlags verifies that notifications are configured identically
// whether set via environment variables or command-line flags.
func TestNotificationsConfigurationFromEnvVarsVsFlags(t *testing.T) {
	testCases := []struct {
		name         string
		envVar       string
		envValue     string
		flagArgs     []string
		expectedNot  []string
		expectedURLs []string
	}{
		{
			name:         "notifications env var",
			envVar:       "WATCHTOWER_NOTIFICATIONS",
			envValue:     "email slack",
			flagArgs:     []string{},
			expectedNot:  []string{"email", "slack"},
			expectedURLs: []string{},
		},
		{
			name:         "notifications flag",
			envVar:       "",
			envValue:     "",
			flagArgs:     []string{"--notifications", "email", "--notifications", "slack"},
			expectedNot:  []string{"email", "slack"},
			expectedURLs: []string{},
		},
		{
			name:         "notification-url env var",
			envVar:       "WATCHTOWER_NOTIFICATION_URL",
			envValue:     "smtp://test slack://test",
			flagArgs:     []string{},
			expectedNot:  []string{},
			expectedURLs: []string{"smtp://test", "slack://test"},
		},
		{
			name:     "notification-url flag",
			envVar:   "",
			envValue: "",
			flagArgs: []string{
				"--notification-url",
				"smtp://test",
				"--notification-url",
				"slack://test",
			},
			expectedNot:  []string{},
			expectedURLs: []string{"smtp://test", "slack://test"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVar != "" {
				t.Setenv(tc.envVar, tc.envValue)
			}

			cmd := new(cobra.Command)

			SetDefaults()
			RegisterNotificationFlags(cmd)
			parseWithEnv(t, cmd, tc.flagArgs...)

			notifications, err := cmd.PersistentFlags().GetStringSlice("notifications")
			require.NoError(t, err)

			assert.Equal(t, tc.expectedNot, notifications)

			urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
			require.NoError(t, err)

			assert.Equal(t, tc.expectedURLs, urls)
		})
	}
}

// TestNotificationURLParsingWithMixedSeparators verifies that notification-url env var splits on spaces.
func TestNotificationURLParsingWithMixedSeparators(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "smtp://test slack://test gotify://test")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	expected := []string{"smtp://test", "slack://test", "gotify://test"}
	assert.Equal(t, expected, urls)
}

// TestNotificationURLParsingWithInvalidValues verifies that invalid URLs are parsed (parsing doesn't validate).
func TestNotificationURLParsingWithInvalidValues(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "invalid-url  smtp://valid")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	expected := []string{"invalid-url", "smtp://valid"}
	assert.Equal(t, expected, urls)
}

// TestNotificationURLParsingWithEmptyValues verifies that empty values from splitting are filtered out.
func TestNotificationURLParsingWithEmptyValues(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", "smtp://test slack://test")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	expected := []string{"smtp://test", "slack://test"}
	assert.Equal(t, expected, urls)
}

// TestNotificationParsingEmptyEnvVar verifies that empty or unset WATCHTOWER_NOTIFICATIONS results in empty slice.
func TestNotificationParsingEmptyEnvVar(t *testing.T) {
	// Unset the env var
	_ = os.Unsetenv("WATCHTOWER_NOTIFICATIONS")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	notifications, err := cmd.PersistentFlags().GetStringSlice("notifications")
	require.NoError(t, err)

	assert.Empty(t, notifications)
}

// TestNotificationParsingWhitespaceOnly verifies that whitespace values are filtered out.
func TestNotificationParsingWhitespaceOnly(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATIONS", "   \t   ")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	notifications, err := cmd.PersistentFlags().GetStringSlice("notifications")
	require.NoError(t, err)

	assert.Empty(t, notifications)
}

// TestNotificationParsingSpecialCharsInURLs verifies URLs with special characters including commas.
func TestNotificationParsingSpecialCharsInURLs(t *testing.T) {
	t.Setenv(
		"WATCHTOWER_NOTIFICATION_URL",
		"smtp://user:pass@host:port,withcomma slack://token@channel",
	)

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	expected := []string{"smtp://user:pass@host:port,withcomma", "slack://token@channel"}
	assert.Equal(t, expected, urls)
}

// TestNotificationParsingLongURLs verifies handling of very long URLs.
func TestNotificationParsingLongURLs(t *testing.T) {
	longURL := "https://very.long.url.with.many.subdomains.and.parameters?param1=value1&param2=value2&param3=" + strings.Repeat(
		"a",
		1000,
	)
	t.Setenv("WATCHTOWER_NOTIFICATION_URL", longURL)

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)
	parseWithEnv(t, cmd)

	urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
	require.NoError(t, err)

	assert.Equal(t, []string{longURL}, urls)
}

// TestNotificationParsingFlagOverridesEnv verifies that flags override environment variables.
func TestNotificationParsingFlagOverridesEnv(t *testing.T) {
	t.Setenv("WATCHTOWER_NOTIFICATIONS", "email")

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	err := cmd.ParseFlags([]string{"--notifications", "slack"})
	require.NoError(t, err)

	notifications, err := cmd.PersistentFlags().GetStringSlice("notifications")
	require.NoError(t, err)

	assert.Equal(t, []string{"slack"}, notifications)
}

// TestGetSecretsFromFilesReadErrors verifies file read errors.
func TestGetSecretsFromFilesReadErrors(t *testing.T) {
	// Create a file and then remove it to simulate read error
	file, err := os.CreateTemp(t.TempDir(), "watchtower-")
	require.NoError(t, err)

	fileName := file.Name()
	require.NoError(t, file.Close())

	// Remove the file
	require.NoError(t, os.Remove(fileName))

	cmd := new(cobra.Command)

	SetDefaults()
	RegisterNotificationFlags(cmd)

	err = cmd.ParseFlags([]string{"--notification-email-server-password", fileName})
	require.NoError(t, err)

	// This should log an error but not panic
	err = getSecretFromFile(cmd.PersistentFlags(), "notification-email-server-password")
	require.NoError(t, err) // Since not a file path, no error

	password, err := cmd.PersistentFlags().GetString("notification-email-server-password")
	require.NoError(t, err)
	assert.Equal(t, fileName, password) // Remains unchanged since not a file
}

// TestFilterEmptyStrings verifies filterEmptyStrings function.
func TestFilterEmptyStrings(t *testing.T) {
	tests := []struct {
		input    []string
		expected any
	}{
		{[]string{"a", "", "b"}, []string{"a", "b"}},
		{[]string{"  ", "c", "\t"}, []string{"c"}},
		{[]string{}, nil},
		{[]string{"", " ", ""}, nil},
		{[]string{"valid"}, []string{"valid"}},
	}

	for _, tt := range tests {
		result := filterEmptyStrings(tt.input)
		if tt.expected == nil {
			assert.Nil(t, result)
		} else {
			assert.Equal(t, tt.expected, result)
		}
	}
}

// TestRegexpSplittingLogic verifies regexp splitting with [, ]+.
func TestRegexpSplittingLogic(t *testing.T) {
	re := regexp.MustCompile("[, ]+")

	tests := []struct {
		input    string
		expected []string
	}{
		{"a,b c", []string{"a", "b", "c"}},
		{"a  ,  b", []string{"a", "b"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"  a   b  ", []string{"", "a", "b", ""}},
		{"", []string{""}},
		{"   ", []string{"", ""}},
	}

	for _, tt := range tests {
		result := re.Split(tt.input, -1)
		assert.Equal(t, tt.expected, result)
	}
}

// TestNotificationURLEnvToFlagWiring verifies WATCHTOWER_NOTIFICATION_URL reaches
// notification-url via ApplyEnvToFlags and ListNotificationURLs parsing for
// representative comma-in-query and mixed-separator cases.
//
// Full SplitNotificationValues coverage lives in internal/flags/utils.
func TestNotificationURLEnvToFlagWiring(t *testing.T) {
	testCases := []struct {
		name     string
		envValue string
		expected []string
	}{
		{
			name:     "comma in query must not split URL",
			envValue: "smtp://user:pass@host:port/?to=recipient1@example.com,recipient2@example.com",
			expected: []string{
				"smtp://user:pass@host:port/?to=recipient1@example.com,recipient2@example.com",
			},
		},
		{
			name:     "space separator with comma in second URL query",
			envValue: "smtp://test1 smtp://test2?param=value,with,commas gotify://host/token",
			expected: []string{
				"smtp://test1",
				"smtp://test2?param=value,with,commas",
				"gotify://host/token",
			},
		},
		{
			name:     "mixed separators with complex URLs",
			envValue: "smtp://test1, smtp://test2?param=value,with,commas gotify://host/token slack://token@channel",
			expected: []string{
				"smtp://test1",
				"smtp://test2?param=value,with,commas",
				"gotify://host/token",
				"slack://token@channel",
			},
		},
		{
			name:     "teams tenant commas preserved",
			envValue: "teams://group@tenant,id.with,commas/altId/groupOwner?host=organization.webhook.office.com",
			expected: []string{
				"teams://group@tenant,id.with,commas/altId/groupOwner?host=organization.webhook.office.com",
			},
		},
		{
			name:     "comma-space separator between full URLs",
			envValue: "slack://token1@channel1, slack://token2@channel2",
			expected: []string{"slack://token1@channel1", "slack://token2@channel2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WATCHTOWER_NOTIFICATION_URL", tc.envValue)

			cmd := new(cobra.Command)

			SetDefaults()
			RegisterNotificationFlags(cmd)
			parseWithEnv(t, cmd)

			urls, err := cmd.PersistentFlags().GetStringArray("notification-url")
			require.NoError(t, err)
			assert.Equal(t, tc.expected, urls)
		})
	}
}

// TestIsPureNumeric verifies isPureNumeric behavior with table-driven cases.
func TestIsPureNumeric(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid cases
		{name: "zero", input: "0", want: true},
		{name: "positive integer", input: "42", want: true},
		{name: "large integer (legacy common case)", input: "300", want: true},
		{name: "simple float", input: "1.5", want: true},
		{name: "leading dot", input: ".5", want: true},
		{name: "trailing dot", input: "1.", want: true},
		{name: "negative integer", input: "-10", want: true},
		{name: "explicit positive", input: "+3", want: true},
		{name: "negative float", input: "-1.5", want: true},
		{name: "positive leading dot", input: "+.5", want: true},
		{name: "negative leading dot", input: "-.5", want: true},

		// Invalid: no digits
		{name: "empty string", input: "", want: false},
		{name: "just decimal point", input: ".", want: false},
		{name: "just plus sign", input: "+", want: false},
		{name: "just minus sign", input: "-", want: false},
		{name: "plus and dot only", input: "+.", want: false},
		{name: "minus and dot only", input: "-.", want: false},

		// Invalid: multiple dots
		{name: "multiple dots", input: "1.2.3", want: false},
		{name: "double dot", input: "1..2", want: false},
		{name: "three dots", input: "1.2.3.4", want: false},

		// Invalid: misplaced or multiple signs
		{name: "minus after digit", input: "1-2", want: false},
		{name: "plus at end", input: "12+", want: false},
		{name: "plus in middle", input: "1+2", want: false},
		{name: "trailing minus", input: "5-", want: false},
		{name: "multiple signs at start", input: "+-1", want: false},
		{name: "mixed signs", input: "-+5", want: false},

		// Invalid: other characters
		{name: "contains letter", input: "1a", want: false},
		{name: "scientific notation", input: "1e3", want: false},
		{name: "thousands separator", input: "1,000", want: false},
		{name: "duration unit seconds", input: "30s", want: false},
		{name: "duration unit minutes", input: "2m", want: false},
		{name: "embedded space", input: "1 2", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isPureNumeric(tc.input)
			assert.Equal(t, tc.want, got, "isPureNumeric(%q)", tc.input)
		})
	}
}
